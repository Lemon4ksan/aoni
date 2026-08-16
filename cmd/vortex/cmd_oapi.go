// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
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

// CmdOAPI provides bidirectional OpenAPI 3.1 and Swagger schema import/export capabilities.
type CmdOAPI struct{}

func (c *CmdOAPI) Name() string      { return "oapi" }
func (c *CmdOAPI) Aliases() []string { return []string{"openapi", "export", "import"} }
func (c *CmdOAPI) Synopsis() string {
	return "OpenAPI 3.1 bidirectional schema toolchain (import/export)"
}
func (c *CmdOAPI) Usage() string { return "vortex oapi [import|export] [flags]" }

func (c *CmdOAPI) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return c.runExport(ctx, nil, stdout, stderr)
	}

	mode := args[0]
	switch mode {
	case "export":
		return c.runExport(ctx, args[1:], stdout, stderr)
	case "import":
		return c.runImport(ctx, args[1:], stdout, stderr)
	default:
		if strings.HasPrefix(mode, "-") {
			return c.runExport(ctx, args, stdout, stderr)
		}

		return fmt.Errorf("unknown oapi command %q. Valid modes: 'import', 'export'", mode)
	}
}

func (c *CmdOAPI) runExport(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("oapi export", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		file        = fs.String("file", "api.go", "Path to Go source file containing @aoni:service contract")
		outFile     = fs.String("out", "openapi.json", "Output OpenAPI specification file path")
		serviceName = fs.String("service", "", "Target service interface name to export (default: all)")
		title       = fs.String("title", "", "API title for OpenAPI spec")
		version     = fs.String("version", "1.0.0", "API version for OpenAPI spec")
		baseURL     = fs.String("base-url", "", "API base URL")
		asYAML      = fs.Bool("yaml", false, "Output spec as YAML instead of JSON")
		vortexFlag  = fs.Bool("vortex", false, "Include Vortex/Aoni vendor extensions (x-vortex) for lossless profiles")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex oapi export — Export Aoni Contract to OpenAPI 3.1 Spec\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(
			stderr,
			"  vortex oapi export -file=<api.go> [-service=Client] [-out=openapi.json] [-yaml] [-vortex]\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return errors.New("vortex oapi export: -file flag is required (e.g. -file=api.go)")
	}

	p := parser.NewParser()

	var root *ir.RootIR

	if *file == "-" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}

		root, err = p.ParseSource("contract.go", src)
		if err != nil {
			return fmt.Errorf("parsing stdin: %w", err)
		}
	} else {
		dir := filepath.Dir(*file)

		pkgRoot, err := p.ParsePackage(dir)
		if err == nil && len(pkgRoot.Services) > 0 {
			root = pkgRoot
		} else {
			root, err = p.ParseFile(*file)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", *file, err)
			}
		}
	}

	exportCfg := openapi.ExportConfig{
		ServiceName: *serviceName,
		Title:       *title,
		Version:     *version,
		BaseURL:     *baseURL,
		AsYAML:      *asYAML,
		Vortex:      *vortexFlag,
	}

	out, err := openapi.ExportOpenAPI(root, exportCfg)
	if err != nil {
		return fmt.Errorf("exporting OpenAPI spec: %w", err)
	}

	if *outFile != "" && *outFile != "-" {
		if err := os.WriteFile(*outFile, out, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", *outFile, err)
		}

		fmt.Fprintf(stdout, "✔ Exported OpenAPI 3.1 specification to %s (%d bytes)\n", *outFile, len(out))

		return nil
	}

	fmt.Fprint(stdout, string(out))

	return nil
}

func (c *CmdOAPI) runImport(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("oapi import", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		specFile       = fs.String("spec", "", "Path to OpenAPI specification file (YAML/JSON, or - for stdin)")
		outFile        = fs.String("out", "api.go", "Output Go contract file path")
		splitModels    = fs.Bool("split", false, "Split generated code into api.go and models.go")
		pkgName        = fs.String("pkg", "api", "Target Go package name")
		serviceName    = fs.String("service", "API", "Target service interface name")
		baseURL        = fs.String("base-url", "", "Override API BaseURL")
		skipDeprecated = fs.Bool("skip-deprecated", false, "Skip deprecated OpenAPI operations")
		overwrite      = fs.Bool("overwrite", false, "Discard existing file and generate contract fresh from scratch")
		prune          = fs.Bool("prune", false, "Prune deleted endpoints instead of adding @deprecated")
		additive       = fs.Bool(
			"additive",
			false,
			"Preserve missing existing endpoints as active instead of marking @deprecated",
		)
		add          = fs.Bool("add", false, "Alias for --additive")
		dryRun       = fs.Bool("dry-run", false, "Preview merge changes without modifying files on disk")
		interactive  = fs.Bool("interactive", false, "Prompt for merge decisions")
		includePaths StringSliceFlag
		excludePaths StringSliceFlag
		typeMaps     StringSliceFlag
	)

	fs.BoolVar(interactive, "i", false, "Prompt for merge decisions (shorthand)")
	fs.Var(&includePaths, "include-path", "Regex pattern to filter included endpoint paths (repeatable)")
	fs.Var(&excludePaths, "exclude-path", "Regex pattern to filter excluded endpoint paths (repeatable)")
	fs.Var(&typeMaps, "type-map", "Custom type mappings (e.g. -type-map=steam_id=id.ID)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex oapi import — Import OpenAPI 3.1/Swagger into Aoni Declarative Contract DSL\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(
			stderr,
			"  vortex oapi import -spec=<swagger.json> [-out=api.go] [-split] [-pkg=api] [-prune] [-overwrite] [-dry-run]\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *specFile == "" {
		return errors.New("vortex oapi import: -spec flag is required (e.g. -spec=swagger.json)")
	}

	var specData []byte
	if *specFile == "-" {
		var err error

		specData, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	}

	doc, err := openapi.LoadSpec(*specFile, specData)
	if err != nil {
		return fmt.Errorf("loading spec: %w", err)
	}

	tm := make(map[string]string)
	for _, mapping := range typeMaps {
		k, v, ok := strings.Cut(mapping, "=")
		if ok {
			tm[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}

	// Check if existing file is present for semantic reconciliation (Git-merge for APIs)
	var existingSrc []byte
	if *outFile != "" && *outFile != "-" && !*overwrite {
		if data, readErr := os.ReadFile(*outFile); readErr == nil && len(data) > 0 {
			existingSrc = data
		}
	}

	if len(existingSrc) > 0 {
		// Run Semantic AST Merge Engine
		mergeEngine := openapi.NewMergeEngine()
		mCfg := openapi.MergeConfig{
			SpecFile:       *specFile,
			PackageName:    *pkgName,
			ServiceName:    *serviceName,
			Prune:          *prune,
			Additive:       *additive || *add,
			Overwrite:      *overwrite,
			DryRun:         *dryRun,
			Interactive:    *interactive,
			SkipDeprecated: *skipDeprecated,
			TypeMap:        tm,
		}

		reconciledSrc, summary, mergeErr := mergeEngine.ReconcileService(existingSrc, doc, mCfg)
		if mergeErr != nil {
			return fmt.Errorf("reconciling contract %s: %w", *outFile, mergeErr)
		}

		fmt.Fprint(stdout, summary.Render(*outFile))

		if !*dryRun {
			if err := os.WriteFile(*outFile, reconciledSrc, 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", *outFile, err)
			}
		}

		return nil
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
			return fmt.Errorf("generating split contract: %w", err)
		}

		outDir := filepath.Dir(*outFile)
		if outDir == "" {
			outDir = "."
		}

		apiPath := *outFile
		modelsPath := filepath.Join(outDir, "models.go")

		if !*dryRun {
			if err := os.WriteFile(apiPath, apiSrc, 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", apiPath, err)
			}

			fmt.Fprintf(stdout, "✔ Generated Aoni API contract in %s (%d bytes)\n", apiPath, len(apiSrc))

			if len(modelsSrc) > 0 {
				if err := os.WriteFile(modelsPath, modelsSrc, 0o600); err != nil {
					return fmt.Errorf("writing %s: %w", modelsPath, err)
				}

				fmt.Fprintf(stdout, "✔ Generated Aoni models in %s (%d bytes)\n", modelsPath, len(modelsSrc))
			}
		} else {
			fmt.Fprintf(
				stdout,
				"[dry-run] Would generate %s (%d bytes) and %s (%d bytes)\n",
				apiPath,
				len(apiSrc),
				modelsPath,
				len(modelsSrc),
			)
		}

		return nil
	}

	src, err := openapi.GenerateContract(doc, cfg)
	if err != nil {
		return fmt.Errorf("generating contract: %w", err)
	}

	if *outFile != "" && *outFile != "-" {
		if !*dryRun {
			if err := os.WriteFile(*outFile, src, 0o600); err != nil {
				return fmt.Errorf("writing output file %s: %w", *outFile, err)
			}

			fmt.Fprintf(stdout, "✔ Generated Aoni contract in %s (%d bytes)\n", *outFile, len(src))
		} else {
			fmt.Fprintf(stdout, "[dry-run] Would generate %s (%d bytes)\n", *outFile, len(src))
		}

		return nil
	}

	fmt.Fprint(stdout, string(src))

	return nil
}
