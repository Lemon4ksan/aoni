// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spec

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// CmdProto compiles Protocol Buffer definitions with high-performance vtprotobuf codecs.
type CmdProto struct{}

func (c *CmdProto) Name() string      { return "proto" }
func (c *CmdProto) Aliases() []string { return []string{"protobuf", "protoc"} }
func (c *CmdProto) Synopsis() string  { return "Compile Protobuf definitions with vtproto codecs" }
func (c *CmdProto) Usage() string     { return "vortex spec proto [flags]" }

func (c *CmdProto) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("proto", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		srcDir     = fs.String("src", "", "Directory containing .proto files (or path to a single .proto)")
		outDir     = fs.String("out", "", "Output directory for generated Go files")
		importPath = fs.String("import", "", "Target Go package import path (e.g. github.com/lemon4ksan/pkg/proto)")
		vtProto    = fs.Bool("vtproto", true, "Generate zero-alloc vtprotobuf fast (un)marshaling routines")
		pkgName    = fs.String("pkg", "", "Override Go package name")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex spec proto — Protobuf & Zero-Allocation Codec Toolchain\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex spec proto -src=<proto_dir> -out=<go_dir> -import=<pkg_path> [-vtproto]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
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

	return c.executeProtoBuild(ctx, stdout, *srcDir, *outDir, *importPath, *pkgName, *vtProto)
}

func (c *CmdProto) executeProtoBuild(
	ctx context.Context,
	stdout io.Writer,
	srcDir, outDir, importPath, pkgName string,
	vtProto bool,
) error {
	protocPath, err := exec.LookPath("protoc")
	if err != nil {
		return fmt.Errorf("'protoc' compiler not found in PATH. Please install protoc: %w", err)
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

	packageRegex := regexp.MustCompile(`(?m)^\s*package\s+([a-zA-Z0-9_]+)\s*;`)
	goPackageRegex := regexp.MustCompile(`(?m)^\s*option\s+go_package\s*=`)

	var processedFiles []string

	for _, file := range files {
		content, rErr := os.ReadFile(file)
		if rErr != nil {
			return fmt.Errorf("reading %s: %w", file, rErr)
		}

		str := string(content)
		if !goPackageRegex.MatchString(str) {
			targetPkg := pkgName
			if targetPkg == "" {
				if matches := packageRegex.FindStringSubmatch(str); len(matches) > 1 {
					targetPkg = matches[1]
				} else {
					targetPkg = filepath.Base(outDir)
				}
			}

			var opt string
			if importPath != "" {
				opt = fmt.Sprintf("option go_package = %q;\n", importPath+";"+targetPkg)
			} else {
				opt = fmt.Sprintf("option go_package = %q;\n", "./;"+targetPkg)
			}

			if loc := packageRegex.FindStringIndex(str); loc != nil {
				str = str[:loc[1]] + "\n" + opt + str[loc[1]:]
			} else {
				str = opt + str
			}
		}

		targetFile := filepath.Join(sandbox, filepath.Base(file))
		if wErr := os.WriteFile(targetFile, []byte(str), 0o600); wErr != nil {
			return fmt.Errorf("writing patched proto %s: %w", targetFile, wErr)
		}

		processedFiles = append(processedFiles, targetFile)
	}

	protocArgs := []string{
		"-I=" + sandbox,
		"--go_out=" + absOut,
		"--go_opt=paths=source_relative",
	}

	if vtProto {
		protocArgs = append(
			protocArgs,
			"--go-vtproto_out="+absOut,
			"--go-vtproto_opt=paths=source_relative",
			"--go-vtproto_opt=features=marshal+unmarshal+size+equal+clone",
		)
	}

	for _, f := range processedFiles {
		protocArgs = append(protocArgs, filepath.Base(f))
	}

	// #nosec G702,G204 -- Controlled compilation invoking local protoc executable
	cmd := exec.CommandContext(ctx, protocPath, protocArgs...)
	cmd.Dir = sandbox

	out, cErr := cmd.CombinedOutput()
	if cErr != nil {
		return fmt.Errorf("protoc compilation failed: %w\n%s", cErr, string(out))
	}

	fmt.Fprintf(stdout, "✔ Successfully compiled %d protobuf definition(s) into %s\n", len(files), absOut)

	return nil
}
