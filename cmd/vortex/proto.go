// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func runProto(args []string) {
	fs := flag.NewFlagSet("vortex proto", flag.ExitOnError)

	var (
		srcDir     = fs.String("src", "", "Directory containing .proto files (or path to a single .proto)")
		outDir     = fs.String("out", "", "Output directory for generated Go files")
		importPath = fs.String("import", "", "Target Go package import path (e.g. github.com/lemon4ksan/pkg/proto)")
		vtProto    = fs.Bool("vtproto", true, "Generate zero-alloc vtprotobuf fast (un)marshaling routines")
		pkgName    = fs.String("pkg", "", "Override Go package name")
	)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "vortex proto — Protobuf & Zero-Allocation Codec Toolchain for Aoni\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  vortex proto -src=<proto_dir> -out=<go_dir> -import=<pkg_path> [-vtproto]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(
			os.Stderr,
			"  vortex proto -src=proto/steam -out=pkg/protobuf/steam -import=github.com/lemon4ksan/g-man/pkg/protobuf/steam\n",
		)
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *srcDir == "" {
		if len(fs.Args()) > 0 {
			*srcDir = fs.Args()[0]
		} else {
			*srcDir = "."
		}
	}

	if *outDir == "" {
		*outDir = *srcDir
	}

	if err := executeProtoBuild(*srcDir, *outDir, *importPath, *pkgName, *vtProto); err != nil {
		fmt.Fprintf(os.Stderr, "vortex proto: %v\n", err)
		os.Exit(1)
	}
}

func executeProtoBuild(srcDir, outDir, importPath, pkgName string, vtProto bool) error {
	protocPath, err := exec.LookPath("protoc")
	if err != nil {
		return fmt.Errorf("'protoc' compiler not found in PATH. Please install protoc to compile protobufs: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(srcDir, "*.proto"))
	if err != nil || len(files) == 0 {
		fi, statErr := os.Stat(srcDir)
		if statErr == nil && !fi.IsDir() && strings.HasSuffix(srcDir, ".proto") {
			files = []string{srcDir}
		} else {
			return fmt.Errorf("no .proto files found in %s", srcDir)
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("failed creating output directory %s: %w", outDir, err)
	}

	absOut, err := filepath.Abs(outDir)
	if err != nil {
		absOut = outDir
	}

	absOut = filepath.ToSlash(absOut)

	tempDir, err := os.MkdirTemp("", "vortex_proto_*")
	if err != nil {
		return fmt.Errorf("failed creating temp directory: %w", err)
	}

	defer os.RemoveAll(tempDir)

	sandbox := filepath.Join(tempDir, "proto")
	_ = os.MkdirAll(sandbox, 0o755)

	pkg := pkgName
	if pkg == "" {
		pkg = filepath.Base(absOut)
	}

	protoFileNames := make([]string, 0, len(files))
	mappings := make([]string, 0, 4*len(files))

	for _, f := range files {
		base := filepath.Base(f)
		protoFileNames = append(protoFileNames, base)
		dst := filepath.Join(sandbox, base)

		copySanitizeProtoFile(f, dst, pkg, importPath)

		if importPath != "" {
			mappings = append(mappings, "--go_opt=M"+base+"="+importPath)
			mappings = append(mappings, "--go_opt=M"+pkg+"/"+base+"="+importPath)

			if vtProto {
				mappings = append(mappings, "--go-vtproto_opt=M"+base+"="+importPath)
				mappings = append(mappings, "--go-vtproto_opt=M"+pkg+"/"+base+"="+importPath)
			}
		}
	}

	var protocArgs []string

	protocArgs = append(protocArgs, "-I=.", "-I=..")
	protocArgs = append(protocArgs, "--go_out="+absOut)
	protocArgs = append(protocArgs, "--go_opt=paths=source_relative")

	if vtProto {
		protocArgs = append(protocArgs, "--go-vtproto_out="+absOut)
		protocArgs = append(protocArgs, "--go-vtproto_opt=paths=source_relative")
		protocArgs = append(protocArgs, "--go-vtproto_opt=features=marshal+unmarshal+size+pool")
	}

	protocArgs = append(protocArgs, mappings...)
	protocArgs = append(protocArgs, protoFileNames...)

	fmt.Printf("📦 Compiling %d Protobuf file(s) -> %s...\n", len(protoFileNames), outDir)

	cmd := exec.CommandContext(context.Background(), protocPath, protocArgs...)
	cmd.Dir = sandbox
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	fmt.Printf("✔ Successfully compiled %d Protobuf file(s) to %s\n", len(protoFileNames), outDir)

	return nil
}

func copySanitizeProtoFile(src, dst, pkgName, importPath string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}

	content := string(data)

	// Replace Google descriptor imports if custom
	content = strings.ReplaceAll(content, "google/protobuf/descriptor.proto", "custom/descriptor.proto")
	content = strings.ReplaceAll(content, "google/protobuf/descriptor.proto", "descriptor.proto")

	// Inject Go package option if absent
	goPkgRx := regexp.MustCompile(`(?m)^option\s+go_package\s*=.*$`)
	if importPath != "" {
		optionStr := fmt.Sprintf("option go_package = %q;", importPath)
		if goPkgRx.MatchString(content) {
			content = goPkgRx.ReplaceAllString(content, optionStr)
		} else {
			syntaxRx := regexp.MustCompile(`(?m)^syntax\s*=.*$`)
			if syntaxRx.MatchString(content) {
				content = syntaxRx.ReplaceAllString(content, "$0\n\n"+optionStr)
			} else {
				content = optionStr + "\n\n" + content
			}
		}
	}

	_ = os.WriteFile(dst, []byte(content), 0o600)
}
