// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func runExport(args []string) {
	fs := flag.NewFlagSet("vortex export", flag.ExitOnError)

	var (
		file        = fs.String("file", "api.go", "Path to Go source file containing @aoni:service contract")
		outFile     = fs.String("out", "openapi.json", "Output OpenAPI specification file path")
		serviceName = fs.String("service", "", "Target service interface name to export (default: all)")
		title       = fs.String("title", "", "API title for OpenAPI spec")
		version     = fs.String("version", "1.0.0", "API version for OpenAPI spec")
		baseURL     = fs.String("base-url", "", "API base URL")
		asYAML      = fs.Bool("yaml", false, "Output spec as YAML instead of JSON")
	)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "vortex export — Export Aoni Declarative Contract into OpenAPI 3.1 Spec\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  vortex export -file=<api.go> [-service=Client] [-out=openapi.json] [-yaml]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  vortex export -file=pkg/services/mannco/api.go -out=openapi.json\n")
		fmt.Fprintf(os.Stderr, "  vortex export -file=pkg/api/api.go -service=Client -out=openapi.yaml -yaml\n")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *file == "" {
		fmt.Fprintf(os.Stderr, "vortex export: -file flag is required (e.g. -file=api.go)\n")
		os.Exit(1)
	}

	p := parser.NewParser()

	var root *ir.RootIR

	if *file == "-" || *file == "" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vortex export: %v\n", err)
			os.Exit(1)
		}

		root, err = p.ParseSource("contract.go", src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vortex export: failed parsing stdin: %v\n", err)
			os.Exit(1)
		}
	} else {
		dir := filepath.Dir(*file)

		pkgRoot, err := p.ParsePackage(dir)
		if err == nil && len(pkgRoot.Services) > 0 {
			root = pkgRoot
		} else {
			root, err = p.ParseFile(*file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "vortex export: failed parsing %s: %v\n", *file, err)
				os.Exit(1)
			}
		}
	}

	exportCfg := openapi.ExportConfig{
		ServiceName: *serviceName,
		Title:       *title,
		Version:     *version,
		BaseURL:     *baseURL,
		AsYAML:      *asYAML,
	}

	out, err := openapi.ExportOpenAPI(root, exportCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vortex export: failed exporting OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	if *outFile != "" && *outFile != "-" {
		if err := os.WriteFile(*outFile, out, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "vortex export: failed writing %s: %v\n", *outFile, err)
			os.Exit(1)
		}

		fmt.Printf("✔ Exported OpenAPI 3.1 specification to %s (%d bytes)\n", *outFile, len(out))

		return
	}

	fmt.Print(string(out))
}

func runImport(args []string) {
	fs := flag.NewFlagSet("vortex import", flag.ExitOnError)

	var (
		specFile       = fs.String("spec", "", "Path to OpenAPI specification file (YAML or JSON, or - for stdin)")
		outFile        = fs.String("out", "api.go", "Output Go contract file path")
		splitModels    = fs.Bool("split", false, "Split generated code into api.go (interface) and models.go (DTOs)")
		pkgName        = fs.String("pkg", "api", "Target Go package name")
		serviceName    = fs.String("service", "API", "Target service interface name")
		baseURL        = fs.String("base-url", "", "Override API BaseURL")
		skipDeprecated = fs.Bool("skip-deprecated", false, "Skip deprecated OpenAPI operations")
		includePaths   stringSliceFlag
		excludePaths   stringSliceFlag
		typeMaps       stringSliceFlag
	)

	fs.Var(&includePaths, "include-path", "Regex pattern to filter included endpoint paths (repeatable)")
	fs.Var(&excludePaths, "exclude-path", "Regex pattern to filter excluded endpoint paths (repeatable)")
	fs.Var(&typeMaps, "type-map", "Custom type mappings (e.g. -type-map=steam_id=id.ID)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "vortex import — Import OpenAPI 3.1/Swagger into Aoni Declarative Contract DSL\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  vortex import -spec=<swagger.json> [-out=api.go] [-split] [-pkg=api]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  vortex import -spec=swagger.json -pkg=bptf -out=pkg/services/bptf/api.go -split\n")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *specFile == "" {
		fmt.Fprintf(os.Stderr, "vortex import: -spec flag is required (e.g. -spec=swagger.json)\n\n")
		fs.Usage()
		os.Exit(1)
	}

	var specData []byte
	if *specFile == "-" {
		var err error

		specData, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vortex import: failed reading stdin: %v\n", err)
			os.Exit(1)
		}
	}

	doc, err := openapi.LoadSpec(*specFile, specData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vortex import: %v\n", err)
		os.Exit(1)
	}

	tm := make(map[string]string)
	for _, mapping := range typeMaps {
		k, v, ok := strings.Cut(mapping, "=")
		if ok {
			tm[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}

	cfg := openapi.ImportConfig{
		SpecFile:       *specFile,
		PackageName:    *pkgName,
		ServiceName:    *serviceName,
		OutputFile:     *outFile,
		BaseURL:        *baseURL,
		SkipDeprecated: *skipDeprecated,
		SplitModels:    *splitModels,
		IncludePaths:   includePaths,
		ExcludePaths:   excludePaths,
		TypeMap:        tm,
	}

	if *splitModels {
		apiSrc, modelsSrc, err := openapi.GenerateSplitContract(doc, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vortex import: failed generating contract: %v\n", err)
			os.Exit(1)
		}

		outDir := filepath.Dir(*outFile)
		if outDir == "" {
			outDir = "."
		}

		apiPath := *outFile
		modelsPath := filepath.Join(outDir, "models.go")

		if err := os.WriteFile(apiPath, apiSrc, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "vortex import: failed writing %s: %v\n", apiPath, err)
			os.Exit(1)
		}

		fmt.Printf("✔ Generated Aoni API contract in %s (%d bytes)\n", apiPath, len(apiSrc))

		if len(modelsSrc) > 0 {
			if err := os.WriteFile(modelsPath, modelsSrc, 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "vortex import: failed writing %s: %v\n", modelsPath, err)
				os.Exit(1)
			}

			fmt.Printf("✔ Generated Aoni models in %s (%d bytes)\n", modelsPath, len(modelsSrc))
		}

		return
	}

	src, err := openapi.GenerateContract(doc, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vortex import: failed generating contract: %v\n", err)
		os.Exit(1)
	}

	if *outFile != "" && *outFile != "-" {
		if err := os.WriteFile(*outFile, src, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "vortex import: failed writing output file %s: %v\n", *outFile, err)
			os.Exit(1)
		}

		fmt.Printf("✔ Generated Aoni contract in %s (%d bytes)\n", *outFile, len(src))

		return
	}

	fmt.Print(string(src))
}
