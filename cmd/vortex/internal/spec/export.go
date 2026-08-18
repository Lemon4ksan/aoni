// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spec

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func (c *Cmd) runExport(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("spec export", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		file        string
		outFile     string
		serviceName string
		title       string
		version     string
		baseURL     string
		asYAML      bool
		vortexFlag  bool
	)

	base.StringVar(fs, &file, "file", "f", "api.go", "Path to Go source file containing @aoni:service contract")
	base.StringVar(fs, &outFile, "out", "o", "openapi.json", "Output OpenAPI specification file path")
	base.StringVar(fs, &serviceName, "service", "s", "", "Target service interface name to export (default: all)")
	base.StringVar(fs, &title, "title", "", "", "API title for OpenAPI spec")
	base.StringVar(fs, &version, "version", "v", "1.0.0", "API version for OpenAPI spec")
	base.StringVar(fs, &baseURL, "base-url", "", "", "API base URL")
	base.BoolVar(fs, &asYAML, "yaml", "y", false, "Output spec as YAML instead of JSON")
	base.BoolVar(
		fs,
		&vortexFlag,
		"vortex",
		"",
		false,
		"Include Vortex/Aoni vendor extensions (x-vortex) for lossless profiles",
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex spec export — Export Aoni Contract to OpenAPI 3.1 Specification\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex spec export [flags]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  vortex spec export -file=./pkg/api/api.go -out=openapi.json\n")
		fmt.Fprintf(stderr, "  vortex spec export -file=./pkg/api/api.go -yaml -out=openapi.yaml\n")
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	targetFile := file
	targetName := ""

	if len(posArgs) > 0 {
		targetFile = posArgs[0]
	}

	rt, _ := base.NewRuntime("")
	if ct, err := rt.ResolveContract(targetFile); err == nil && ct != nil {
		targetFile = ct.AbsPath

		targetName = ct.Name
		if serviceName == "" {
			serviceName = ct.Service
		}
	}

	if targetFile == "" {
		return errors.New(
			"vortex spec export: target contract name or -file flag is required (e.g. `vortex spec export AntigravityAPI` or `-file=api.go`)",
		)
	}

	p := parser.NewParser()

	var root *ir.RootIR

	if targetFile == "-" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}

		root, err = p.ParseSource("contract.go", src)
		if err != nil {
			return fmt.Errorf("parsing stdin: %w", err)
		}
	} else {
		dir := filepath.Dir(targetFile)

		pkgRoot, err := p.ParsePackage(dir)
		if err == nil && len(pkgRoot.Services) > 0 {
			root = pkgRoot
		} else {
			root, err = p.ParseFile(targetFile)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", targetFile, err)
			}
		}
	}

	exportTitle := title
	if exportTitle == "" && targetName != "" {
		exportTitle = targetName
	}

	exportCfg := openapi.ExportConfig{
		ServiceName: serviceName,
		Title:       exportTitle,
		Version:     version,
		BaseURL:     baseURL,
		AsYAML:      asYAML,
		Vortex:      vortexFlag,
	}

	out, err := openapi.ExportOpenAPI(root, exportCfg)
	if err != nil {
		return fmt.Errorf("exporting OpenAPI spec: %w", err)
	}

	if outFile != "" && outFile != "-" {
		if err := os.WriteFile(outFile, out, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", outFile, err)
		}

		fmt.Fprintf(stdout, "✔ Exported OpenAPI 3.1 specification to %s (%d bytes)\n", outFile, len(out))

		return nil
	}

	fmt.Fprint(stdout, string(out))

	return nil
}
