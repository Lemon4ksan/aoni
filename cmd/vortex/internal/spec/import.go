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
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/jsbundle"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/openapi"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/pipeline"
)

type importOptions struct {
	specFile       string
	outFile        string
	pkgName        string
	serviceName    string
	baseURL        string
	mergeMode      string
	jsFlag         string
	resolvedMode   openapi.MergeMode
	targetOut      string
	targetPkg      string
	targetService  string
	inputSpec      string
	specList       []string
	includePaths   base.StringSliceFlag
	excludePaths   base.StringSliceFlag
	typeMaps       base.StringSliceFlag
	typeMapParsed  map[string]string
	splitModels    bool
	skipDeprecated bool
	force          bool
	prune          bool
	add            bool
	cacheFlag      bool
	moveFlag       bool
	dryRun         bool
	interactive    bool
}

func (c *Cmd) runImport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts, err := parseImportOptions(args, stderr)
	if err != nil {
		return err
	}

	resolveImportTargets(opts, stdout)

	if opts.inputSpec == "" && opts.jsFlag == "" {
		return errors.New(
			"vortex spec import: spec file or -js flag is required (e.g. `vortex spec import session.har` or `vortex spec import -js=\"*.js\"`)",
		)
	}

	if opts.inputSpec == "" && opts.jsFlag != "" {
		return runJSBundleImport(opts, stdout)
	}

	return runOpenAPIImport(ctx, opts, stdout)
}

func parseImportOptions(args []string, stderr io.Writer) (*importOptions, error) {
	fs := flag.NewFlagSet("spec import", flag.ContinueOnError)
	fs.SetOutput(stderr)

	opts := &importOptions{}

	base.StringVar(
		fs,
		&opts.specFile,
		"spec",
		"s",
		"",
		"Path to OpenAPI specification file (YAML/JSON, or - for stdin)",
	)
	base.StringVar(fs, &opts.outFile, "out", "o", "api.go", "Output Go contract file path or registered contract name")
	base.BoolVar(fs, &opts.splitModels, "split", "", false, "Split generated code into api.go and models.go")
	base.StringVar(fs, &opts.pkgName, "pkg", "p", "api", "Target Go package name")
	base.StringVar(fs, &opts.serviceName, "service", "", "API", "Target service interface name")
	base.StringVar(fs, &opts.baseURL, "base-url", "", "", "Override API BaseURL")
	base.BoolVar(fs, &opts.skipDeprecated, "skip-deprecated", "", false, "Skip deprecated OpenAPI operations")
	base.BoolVar(
		fs,
		&opts.force,
		"force",
		"f",
		false,
		"Discard existing file and generate contract fresh from scratch (alias: --overwrite)",
	)
	fs.BoolVar(&opts.force, "overwrite", false, "Alias for --force")
	base.BoolVar(fs, &opts.prune, "prune", "", false, "Prune deleted endpoints instead of adding @deprecated")
	base.StringVar(
		fs,
		&opts.mergeMode,
		"mode",
		"m",
		"union",
		"Merge strategy when combining multiple specs: union, intersect, diff",
	)
	base.BoolVar(
		fs,
		&opts.add,
		"add",
		"a",
		false,
		"Preserve missing existing endpoints as active instead of marking @deprecated (alias: --additive)",
	)
	fs.BoolVar(&opts.add, "additive", false, "Alias for --add")
	base.BoolVar(
		fs,
		&opts.cacheFlag,
		"cache",
		"",
		false,
		"Archive HAR files into .vortex/cache and store credentials in vault",
	)
	base.BoolVar(
		fs,
		&opts.moveFlag,
		"move",
		"",
		false,
		"Move original HAR files into cache (deletes source file on success)",
	)
	base.BoolVar(fs, &opts.dryRun, "dry-run", "", false, "Preview merge changes without modifying files on disk")
	base.BoolVar(fs, &opts.interactive, "interactive", "i", false, "Prompt for merge decisions")
	base.StringVar(
		fs,
		&opts.jsFlag,
		"js",
		"",
		"",
		"Optional JavaScript bundle path or glob to enrich endpoints and schemas",
	)

	fs.Var(&opts.includePaths, "include-path", "Regex pattern to filter included endpoint paths (repeatable)")
	fs.Var(&opts.excludePaths, "exclude-path", "Regex pattern to filter excluded endpoint paths (repeatable)")
	fs.Var(&opts.typeMaps, "type-map", "Custom type mappings (e.g. -type-map=steam_id=id.ID)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex spec import — Import OpenAPI/Swagger or HAR Traffic with 3-Way AST Merge\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex spec import [flags]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(
			stderr,
			"  vortex spec import -spec=openapi.json -out=./pkg/api/api.go         # Fresh import or sync\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex spec import -spec=session.har -out=./pkg/api/api.go -dry-run # Preview HAR diff\n",
		)
		fmt.Fprintf(stderr, "  vortex spec import -spec=session.har -out=./pkg/api/api.go -add     # Additive merge\n")
		fmt.Fprintf(
			stderr,
			"  vortex spec import -spec=session.har -js=\"*.js\" -out=./pkg/api/api.go # JS-enriched import\n",
		)
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return nil, err
	}

	modeVal := strings.ToLower(strings.TrimSpace(opts.mergeMode))
	switch modeVal {
	case "intersect", "intersection":
		opts.resolvedMode = openapi.MergeModeIntersection
	case "diff", "difference":
		opts.resolvedMode = openapi.MergeModeDifference
	default:
		opts.resolvedMode = openapi.MergeModeUnion
	}

	opts.targetOut = opts.outFile
	opts.targetPkg = opts.pkgName
	opts.targetService = opts.serviceName

	if opts.specFile != "" {
		for _, s := range strings.Split(opts.specFile, ",") {
			if strings.TrimSpace(s) != "" {
				opts.specList = append(opts.specList, strings.TrimSpace(s))
			}
		}
	}

	for len(posArgs) > 0 {
		arg := posArgs[0]
		isSpec := false

		for _, item := range strings.Split(arg, ",") {
			item = strings.TrimSpace(item)
			if strings.HasSuffix(item, ".har") || strings.HasSuffix(item, ".json") ||
				strings.HasSuffix(item, ".yaml") || strings.HasSuffix(item, ".yml") ||
				strings.HasPrefix(item, "cache:") || strings.ContainsAny(item, "*?[]") {
				isSpec = true

				opts.specList = append(opts.specList, item)
			}
		}

		if isSpec {
			posArgs = posArgs[1:]
			continue
		}

		if arg != ".go" && arg != "." && arg != "" {
			opts.targetOut = arg
		}

		break
	}

	if opts.targetOut == ".go" || opts.targetOut == "" || opts.targetOut == "." {
		opts.targetOut = "api.go"
	}

	opts.inputSpec = strings.Join(opts.specList, ",")

	opts.typeMapParsed = make(map[string]string)
	for _, mapping := range opts.typeMaps {
		k, v, ok := strings.Cut(mapping, "=")
		if ok {
			opts.typeMapParsed[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}

	return opts, nil
}

func resolveImportTargets(opts *importOptions, stdout io.Writer) {
	// Auto-discovery if spec is not explicitly specified and JS bundle is not provided
	if opts.inputSpec == "" && opts.jsFlag == "" {
		candidates, _ := filepath.Glob("*.har")
		if len(candidates) == 0 {
			candidates, _ = filepath.Glob("*.json")
		}

		if len(candidates) > 0 {
			var (
				latestFile string
				latestTime time.Time
			)

			for _, c := range candidates {
				if strings.HasSuffix(c, ".gen.go") || strings.HasSuffix(c, ".yml") || strings.HasSuffix(c, ".yaml") {
					continue
				}

				if fi, err := os.Stat(c); err == nil && fi.ModTime().After(latestTime) {
					latestTime = fi.ModTime()
					latestFile = c
				}
			}

			if latestFile != "" {
				opts.inputSpec = latestFile
				fmt.Fprintf(stdout, "💡 Auto-detected upstream spec: %s\n", latestFile)
			}
		}
	}

	rt, _ := base.NewRuntime("")
	if ct, err := rt.ResolveContract(opts.targetOut); err == nil && ct != nil {
		opts.targetOut = ct.AbsPath
		if opts.targetPkg == "api" && ct.Package != "" {
			opts.targetPkg = ct.Package
		}

		if opts.targetService == "API" && ct.Service != "" {
			opts.targetService = ct.Service
		}
	}

	if opts.targetOut != "-" && filepath.Ext(opts.targetOut) == "" {
		opts.targetOut += ".go"
	}

	if opts.targetPkg == "api" && opts.targetOut != "" {
		dir := filepath.Dir(opts.targetOut)
		if dir != "" && dir != "." {
			baseDir := filepath.Base(dir)
			if baseDir != "" && baseDir != "." && baseDir != "pkg" &&
				baseDir != "internal" && isValidGoPackageName(baseDir) {
				opts.targetPkg = baseDir
			}
		}
	}
}

func runJSBundleImport(opts *importOptions, stdout io.Writer) error {
	var jsGlobs []string
	for _, p := range strings.Split(opts.jsFlag, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			jsGlobs = append(jsGlobs, p)
		}
	}

	scan, sErr := jsbundle.ScanFiles(jsGlobs)
	if sErr != nil {
		return fmt.Errorf("scanning js bundles: %w", sErr)
	}

	if len(scan.Endpoints) == 0 {
		return fmt.Errorf("no endpoints discovered in javascript bundles matching %s", opts.jsFlag)
	}

	fmt.Fprintf(
		stdout,
		"Discovered %d RPC endpoints & %d messages from JS bundles\n",
		len(scan.Endpoints),
		len(scan.Messages),
	)

	var existingAPISrc []byte
	if !opts.force {
		if data, readErr := os.ReadFile(opts.targetOut); readErr == nil && len(data) > 0 {
			existingAPISrc = data
		}
	}

	var contractBytes []byte
	if len(existingAPISrc) > 0 {
		doc := jsbundle.ScanToOpenAPI(scan, opts.baseURL)
		engine := openapi.NewMergeEngine()

		mergedBytes, summary, mErr := engine.ReconcileService(existingAPISrc, doc, openapi.MergeConfig{
			SpecFile:    "javascript-bundle",
			PackageName: opts.targetPkg,
			ServiceName: opts.targetService,
			Prune:       opts.prune,
			Additive:    opts.add || !opts.prune,
		})
		if mErr != nil {
			return fmt.Errorf("reconciling contract from js: %w", mErr)
		}

		contractBytes = mergedBytes

		if summary != nil {
			fmt.Fprint(stdout, summary.Render(opts.targetOut))
		}
	} else {
		var gErr error

		contractBytes, gErr = jsbundle.GenerateContract(scan, jsbundle.ContractOptions{
			PackageName: opts.targetPkg,
			ServiceName: opts.targetService,
			BaseURL:     opts.baseURL,
			Engine:      "fast",
		})
		if gErr != nil {
			return fmt.Errorf("generating contract from js: %w", gErr)
		}
	}

	if opts.dryRun {
		fmt.Fprintf(stdout, "%s\n", string(contractBytes))
		return nil
	}

	_ = os.MkdirAll(filepath.Dir(opts.targetOut), 0o755)
	if err := os.WriteFile(opts.targetOut, contractBytes, 0o600); err != nil {
		return fmt.Errorf("writing contract %s: %w", opts.targetOut, err)
	}

	fmt.Fprintf(stdout, "✔ Generated/Reconciled Aoni contract in %s (%d bytes)\n", opts.targetOut, len(contractBytes))

	return nil
}

func runOpenAPIImport(_ context.Context, opts *importOptions, stdout io.Writer) error {
	var specData []byte
	if opts.inputSpec == "-" {
		var err error

		specData, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	}

	rt, _ := base.NewRuntime("")

	// Cache and sanitize raw HAR captures and extract credentials into vault
	if opts.cacheFlag || opts.moveFlag {
		tp, _ := pipeline.NewTrafficPipeline(rt.RootDir)
		for _, sPath := range opts.specList {
			if strings.HasSuffix(sPath, ".har") {
				_, _, _ = tp.IngestHAR(sPath, nil, opts.moveFlag, true)
			}
		}
	}

	if opts.jsFlag != "" {
		var jsGlobs []string
		for _, p := range strings.Split(opts.jsFlag, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				jsGlobs = append(jsGlobs, p)
			}
		}

		if jsScan, jErr := jsbundle.ScanFiles(jsGlobs); jErr == nil && jsScan != nil && len(jsScan.Endpoints) > 0 {
			fmt.Fprintf(
				stdout,
				"Discovered %d RPC endpoints & %d messages from JS bundles\n",
				len(jsScan.Endpoints),
				len(jsScan.Messages),
			)
		}
	}

	doc, err := openapi.LoadSpecWithMode(opts.inputSpec, specData, opts.resolvedMode)
	if err != nil {
		return fmt.Errorf("loading spec: %w", err)
	}

	// Check if existing file is present for semantic reconciliation (Git-merge for APIs)
	var existingSrc []byte
	if opts.targetOut != "" && opts.targetOut != "-" && !opts.force {
		if data, readErr := os.ReadFile(opts.targetOut); readErr == nil && len(data) > 0 {
			existingSrc = data
		}
	}

	if len(existingSrc) > 0 {
		return reconcileExistingContract(existingSrc, doc, opts, stdout)
	}

	return generateAndWriteContract(doc, opts, stdout)
}

func reconcileExistingContract(existingSrc []byte, doc *openapi.Document, opts *importOptions, stdout io.Writer) error {
	mergeEngine := openapi.NewMergeEngine()
	mCfg := openapi.MergeConfig{
		SpecFile:       opts.inputSpec,
		PackageName:    opts.targetPkg,
		ServiceName:    opts.targetService,
		Prune:          opts.prune,
		Additive:       opts.add,
		Overwrite:      opts.force,
		DryRun:         opts.dryRun,
		Interactive:    opts.interactive,
		SkipDeprecated: opts.skipDeprecated,
		TypeMap:        opts.typeMapParsed,
	}

	reconciledSrc, summary, mergeErr := mergeEngine.ReconcileService(existingSrc, doc, mCfg)
	if mergeErr != nil {
		return fmt.Errorf("reconciling contract %s: %w", opts.targetOut, mergeErr)
	}

	fmt.Fprint(stdout, summary.Render(opts.targetOut))

	if !opts.dryRun {
		if err := os.WriteFile(opts.targetOut, reconciledSrc, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", opts.targetOut, err)
		}
	}

	return nil
}

func generateAndWriteContract(doc *openapi.Document, opts *importOptions, stdout io.Writer) error {
	cfg := openapi.ImportConfig{
		SpecFile:       opts.inputSpec,
		PackageName:    opts.targetPkg,
		ServiceName:    opts.targetService,
		OutputFile:     opts.targetOut,
		BaseURL:        opts.baseURL,
		SkipDeprecated: opts.skipDeprecated,
		SplitModels:    opts.splitModels,
		IncludePaths:   opts.includePaths,
		ExcludePaths:   opts.excludePaths,
		TypeMap:        opts.typeMapParsed,
		MergeMode:      opts.resolvedMode,
	}

	if opts.splitModels {
		apiSrc, modelsSrc, err := openapi.GenerateSplitContract(doc, cfg)
		if err != nil {
			return fmt.Errorf("generating split contract: %w", err)
		}

		outDir := filepath.Dir(opts.targetOut)
		if outDir == "" {
			outDir = "."
		}

		apiPath := opts.targetOut
		modelsPath := filepath.Join(outDir, "models.go")

		if !opts.dryRun {
			if outDir != "." {
				_ = os.MkdirAll(outDir, 0o755)
			}

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

	if opts.targetOut != "" && opts.targetOut != "-" {
		if !opts.dryRun {
			dir := filepath.Dir(opts.targetOut)
			if dir != "" && dir != "." {
				_ = os.MkdirAll(dir, 0o755)
			}

			if err := os.WriteFile(opts.targetOut, src, 0o600); err != nil {
				return fmt.Errorf("writing output file %s: %w", opts.targetOut, err)
			}

			fmt.Fprintf(stdout, "✔ Generated Aoni contract in %s (%d bytes)\n", opts.targetOut, len(src))
		} else {
			fmt.Fprintf(stdout, "[dry-run] Would generate %s (%d bytes)\n", opts.targetOut, len(src))
		}

		return nil
	}

	fmt.Fprint(stdout, string(src))

	return nil
}

func isValidGoPackageName(name string) bool {
	if len(name) == 0 {
		return false
	}

	for i, r := range name {
		if i == 0 && (r >= '0' && r <= '9') {
			return false
		}

		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}

	return true
}
