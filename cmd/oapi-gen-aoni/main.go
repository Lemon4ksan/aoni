// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "export" {
		runExport(os.Args[2:])
		return
	}

	runImport(os.Args[1:])
}

func runImport(args []string) {
	fs := flag.NewFlagSet("oapi-gen-aoni", flag.ExitOnError)

	var (
		specFile       = fs.String("spec", "", "Path to OpenAPI specification file (YAML or JSON, or - for stdin)")
		outFile        = fs.String("out", "api.go", "Output Go contract file path")
		pkgName        = fs.String("pkg", "api", "Target Go package name")
		serviceName    = fs.String("service", "Client", "Target service interface name")
		baseURL        = fs.String("base-url", "", "Override API BaseURL")
		skipDeprecated = fs.Bool("skip-deprecated", false, "Skip deprecated OpenAPI operations")
		includePaths   stringSliceFlag
		excludePaths   stringSliceFlag
	)

	fs.Var(&includePaths, "include-path", "Regex pattern to filter included endpoint paths (repeatable)")
	fs.Var(&excludePaths, "exclude-path", "Regex pattern to filter excluded endpoint paths (repeatable)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni — Bidirectional OpenAPI ↔ Aoni Contract DSL Generator\n\n")
		fmt.Fprintf(os.Stderr, "Usage (Import: OpenAPI -> Aoni Declarative Contract):\n")
		fmt.Fprintf(os.Stderr, "  oapi-gen-aoni -spec=<swagger.json> [-out=api.go] [-pkg=api]\n\n")
		fmt.Fprintf(os.Stderr, "Usage (Export: Aoni Declarative Contract -> OpenAPI 3.1 Spec):\n")
		fmt.Fprintf(os.Stderr, "  oapi-gen-aoni export -file=<api.go> [-out=openapi.json] [-yaml]\n\n")
		fmt.Fprintf(os.Stderr, "Flags (Import):\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  oapi-gen-aoni -spec=swagger.json -pkg=bptf -out=pkg/services/bptf/api.go\n")
		fmt.Fprintf(os.Stderr, "  oapi-gen-aoni export -file=pkg/services/bptf/api.go -out=swagger.json\n")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *specFile == "" {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni: -spec flag is required (e.g. -spec=swagger.json)\n\n")
		fs.Usage()
		os.Exit(1)
	}

	var specData []byte
	if *specFile == "-" {
		var err error

		specData, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "oapi-gen-aoni: failed reading stdin: %v\n", err)
			os.Exit(1)
		}
	}

	doc, err := openapi.LoadSpec(*specFile, specData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni: %v\n", err)
		os.Exit(1)
	}

	cfg := openapi.ImportConfig{
		SpecFile:       *specFile,
		PackageName:    *pkgName,
		ServiceName:    *serviceName,
		OutputFile:     *outFile,
		BaseURL:        *baseURL,
		SkipDeprecated: *skipDeprecated,
		IncludePaths:   includePaths,
		ExcludePaths:   excludePaths,
	}

	src, err := openapi.GenerateContract(doc, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni: failed generating contract: %v\n", err)
		os.Exit(1)
	}

	if *outFile != "" && *outFile != "-" {
		if err := os.WriteFile(*outFile, src, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "oapi-gen-aoni: failed writing output file %s: %v\n", *outFile, err)
			os.Exit(1)
		}

		fmt.Printf("✔ Generated Aoni contract in %s (%d bytes)\n", *outFile, len(src))

		return
	}

	fmt.Print(string(src))
}

func runExport(args []string) {
	fs := flag.NewFlagSet("oapi-gen-aoni export", flag.ExitOnError)

	var (
		file    = fs.String("file", "api.go", "Path to Go source file containing @aoni:service contract")
		outFile = fs.String("out", "openapi.json", "Output OpenAPI specification file path")
		title   = fs.String("title", "", "API title for OpenAPI spec")
		version = fs.String("version", "1.0.0", "API version for OpenAPI spec")
		baseURL = fs.String("base-url", "", "API base URL")
		asYAML  = fs.Bool("yaml", false, "Output spec as YAML instead of JSON")
	)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni export — Export Aoni Declarative Contract into OpenAPI 3.1 Spec\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  oapi-gen-aoni export -file=<api.go> [-out=openapi.json] [-yaml]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  oapi-gen-aoni export -file=pkg/api/api.go -out=openapi.json\n")
		fmt.Fprintf(os.Stderr, "  oapi-gen-aoni export -file=pkg/api/api.go -out=openapi.yaml -yaml\n")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *file == "" {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni export: -file flag is required (e.g. -file=api.go)\n")
		os.Exit(1)
	}

	var (
		src []byte
		err error
	)

	if *file == "-" || *file == "" {
		src, err = io.ReadAll(os.Stdin)
	} else {
		src, err = os.ReadFile(*file)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni export: %v\n", err)
		os.Exit(1)
	}

	p := parser.NewParser()

	root, err := p.ParseSource("contract.go", src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni export: failed parsing %s: %v\n", *file, err)
		os.Exit(1)
	}

	exportCfg := openapi.ExportConfig{
		Title:   *title,
		Version: *version,
		BaseURL: *baseURL,
		AsYAML:  *asYAML,
	}

	out, err := openapi.ExportOpenAPI(root, exportCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oapi-gen-aoni export: failed exporting OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	if *outFile != "" && *outFile != "-" {
		if err := os.WriteFile(*outFile, out, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "oapi-gen-aoni export: failed writing %s: %v\n", *outFile, err)
			os.Exit(1)
		}

		fmt.Printf("✔ Exported OpenAPI 3.1 specification to %s (%d bytes)\n", *outFile, len(out))

		return
	}

	fmt.Print(string(out))
}
