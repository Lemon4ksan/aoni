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
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdOAPI provides bidirectional OpenAPI 3.1 and Swagger schema import/export capabilities.
type CmdOAPI struct{}

func (c *CmdOAPI) Name() string      { return "oapi" }
func (c *CmdOAPI) Aliases() []string { return []string{"openapi"} }
func (c *CmdOAPI) Synopsis() string {
	return "OpenAPI 3.1 bidirectional schema toolchain (import/export)"
}
func (c *CmdOAPI) Usage() string { return "vortex oapi <import|export> [flags]" }

func (c *CmdOAPI) printUsage(w io.Writer) {
	fmt.Fprintf(w, "vortex oapi — OpenAPI 3.1 & Swagger Bidirectional Schema Toolchain\n\n")
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  vortex oapi <import|export> [flags]\n\n")
	fmt.Fprintf(w, "Subcommands:\n")
	fmt.Fprintf(w, "  import    Import OpenAPI/Swagger or HAR traffic into Go contract with 3-way AST merge\n")
	fmt.Fprintf(w, "  export    Export @aoni:service Go contracts into OpenAPI 3.1 specifications\n\n")
	fmt.Fprintf(w, "Examples:\n")
	fmt.Fprintf(w, "  vortex oapi import -spec=openapi.json -out=./pkg/api/api.go\n")
	fmt.Fprintf(w, "  vortex oapi import -spec=session.har -out=./pkg/api/api.go -add\n")
	fmt.Fprintf(w, "  vortex oapi export -file=./pkg/api/api.go -out=openapi.json\n\n")
	fmt.Fprintf(w, "Run 'vortex oapi import -h' or 'vortex oapi export -h' for subcommand options.\n")
}

func (c *CmdOAPI) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		c.printUsage(stderr)
		return nil
	}

	mode := args[0]
	switch mode {
	case "export":
		return c.runExport(ctx, args[1:], stdout, stderr)
	case "import":
		return c.runImport(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		c.printUsage(stdout)
		return nil
	default:
		c.printUsage(stderr)
		return fmt.Errorf("unknown oapi subcommand %q. Valid modes: 'import', 'export'", mode)
	}
}

// CmdImport imports OpenAPI, Swagger, or HAR traffic archives directly into Go declarative contracts.
type CmdImport struct {
	oapi CmdOAPI
}

func (c *CmdImport) Name() string      { return "import" }
func (c *CmdImport) Aliases() []string { return []string{"ingest"} }
func (c *CmdImport) Synopsis() string {
	return "Import OpenAPI, Swagger, or HAR traffic into Go declarative contract DSL"
}
func (c *CmdImport) Usage() string { return "vortex import -spec=<file> [flags]" }
func (c *CmdImport) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return c.oapi.runImport(ctx, args, stdout, stderr)
}

// CmdExport exports @aoni:service Go contracts into standard OpenAPI 3.1 specifications.
type CmdExport struct {
	oapi CmdOAPI
}

func (c *CmdExport) Name() string      { return "export" }
func (c *CmdExport) Aliases() []string { return nil }
func (c *CmdExport) Synopsis() string {
	return "Export Aoni Go contracts into OpenAPI 3.1 JSON/YAML specifications"
}
func (c *CmdExport) Usage() string { return "vortex export -file=<api.go> [flags]" }
func (c *CmdExport) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return c.oapi.runExport(ctx, args, stdout, stderr)
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
		fmt.Fprintf(stderr, "vortex export — Export Aoni Contract to OpenAPI 3.1 Specification\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex export [flags]\n")
		fmt.Fprintf(stderr, "  vortex oapi export [flags]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  vortex export -file=./pkg/api/api.go -out=openapi.json\n")
		fmt.Fprintf(stderr, "  vortex export -file=./pkg/api/api.go -yaml -out=openapi.yaml\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	targetFile := *file
	targetName := ""

	if len(fs.Args()) > 0 {
		targetFile = fs.Args()[0]
	}

	// Try finding contract in .vortex.yml to extract canonical name and file path
	cwd, _ := os.Getwd()
	if cfg, err := project.Load(cwd); err == nil && cfg != nil {
		if c := cfg.FindContract(targetFile); c != nil {
			if c.File != "" {
				targetFile = c.File
				if !filepath.IsAbs(targetFile) && cfg.RootDir != "" {
					targetFile = filepath.Join(cfg.RootDir, targetFile)
				}
			}

			targetName = c.Name
		}
	}

	if targetName == "" {
		if resolved := project.ResolveTargetToPath(targetFile); resolved != "" {
			targetFile = resolved
		}
	}

	if targetFile == "" {
		return errors.New(
			"vortex export: target contract name or -file flag is required (e.g. `vortex export AntigravityAPI` or `-file=api.go`)",
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

	exportTitle := *title
	if exportTitle == "" && targetName != "" {
		exportTitle = targetName
	}

	exportCfg := openapi.ExportConfig{
		ServiceName: *serviceName,
		Title:       exportTitle,
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
		fmt.Fprintf(stderr, "vortex import — Import OpenAPI/Swagger or HAR Traffic with 3-Way AST Merge\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex import [flags]\n")
		fmt.Fprintf(stderr, "  vortex oapi import [flags]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(
			stderr,
			"  vortex import -spec=openapi.json -out=./pkg/api/api.go         # Fresh import or sync\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex import -spec=session.har -out=./pkg/api/api.go -dry-run # Preview HAR diff\n",
		)
		fmt.Fprintf(stderr, "  vortex import -spec=session.har -out=./pkg/api/api.go -add     # Additive merge\n")
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
