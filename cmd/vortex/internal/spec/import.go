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
	"github.com/lemon4ksan/aoni/internal/codegen/jsbundle"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/pipeline"
)

func (c *Cmd) runImport(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("spec import", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		specFile       string
		outFile        string
		splitModels    bool
		pkgName        string
		serviceName    string
		baseURL        string
		skipDeprecated bool
		force          bool
		prune          bool
		mergeMode      string
		add            bool
		cacheFlag      bool
		moveFlag       bool
		dryRun         bool
		interactive    bool
		jsFlag         string
		includePaths   base.StringSliceFlag
		excludePaths   base.StringSliceFlag
		typeMaps       base.StringSliceFlag
	)

	base.StringVar(fs, &specFile, "spec", "s", "", "Path to OpenAPI specification file (YAML/JSON, or - for stdin)")
	base.StringVar(fs, &outFile, "out", "o", "api.go", "Output Go contract file path or registered contract name")
	base.BoolVar(fs, &splitModels, "split", "", false, "Split generated code into api.go and models.go")
	base.StringVar(fs, &pkgName, "pkg", "p", "api", "Target Go package name")
	base.StringVar(fs, &serviceName, "service", "", "API", "Target service interface name")
	base.StringVar(fs, &baseURL, "base-url", "", "", "Override API BaseURL")
	base.BoolVar(fs, &skipDeprecated, "skip-deprecated", "", false, "Skip deprecated OpenAPI operations")
	base.BoolVar(
		fs,
		&force,
		"force",
		"f",
		false,
		"Discard existing file and generate contract fresh from scratch (alias: --overwrite)",
	)
	fs.BoolVar(&force, "overwrite", false, "Alias for --force")
	base.BoolVar(fs, &prune, "prune", "", false, "Prune deleted endpoints instead of adding @deprecated")
	base.StringVar(
		fs,
		&mergeMode,
		"mode",
		"m",
		"union",
		"Merge strategy when combining multiple specs: union, intersect, diff",
	)
	base.BoolVar(
		fs,
		&add,
		"add",
		"a",
		false,
		"Preserve missing existing endpoints as active instead of marking @deprecated (alias: --additive)",
	)
	fs.BoolVar(&add, "additive", false, "Alias for --add")
	base.BoolVar(
		fs,
		&cacheFlag,
		"cache",
		"",
		false,
		"Archive HAR files into .vortex/cache and store credentials in vault",
	)
	base.BoolVar(
		fs,
		&moveFlag,
		"move",
		"",
		false,
		"Move original HAR files into cache (deletes source file on success)",
	)
	base.BoolVar(fs, &dryRun, "dry-run", "", false, "Preview merge changes without modifying files on disk")
	base.BoolVar(fs, &interactive, "interactive", "i", false, "Prompt for merge decisions")
	base.StringVar(
		fs,
		&jsFlag,
		"js",
		"",
		"",
		"Optional JavaScript bundle path or glob to enrich endpoints and schemas",
	)

	fs.Var(&includePaths, "include-path", "Regex pattern to filter included endpoint paths (repeatable)")
	fs.Var(&excludePaths, "exclude-path", "Regex pattern to filter excluded endpoint paths (repeatable)")
	fs.Var(&typeMaps, "type-map", "Custom type mappings (e.g. -type-map=steam_id=id.ID)")

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
		return err
	}

	var resolvedMode openapi.MergeMode

	modeVal := strings.ToLower(strings.TrimSpace(mergeMode))

	switch modeVal {
	case "intersect", "intersection":
		resolvedMode = openapi.MergeModeIntersection
	case "diff", "difference":
		resolvedMode = openapi.MergeModeDifference
	default:
		resolvedMode = openapi.MergeModeUnion
	}

	targetOut := outFile
	targetPkg := pkgName
	targetService := serviceName

	var specList []string
	if specFile != "" {
		for _, s := range strings.Split(specFile, ",") {
			if strings.TrimSpace(s) != "" {
				specList = append(specList, strings.TrimSpace(s))
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

				specList = append(specList, item)
			}
		}

		if isSpec {
			posArgs = posArgs[1:]
			continue
		}

		if arg != ".go" && arg != "." && arg != "" {
			targetOut = arg
		}

		break
	}

	if targetOut == ".go" || targetOut == "" || targetOut == "." {
		targetOut = "api.go"
	}

	inputSpec := strings.Join(specList, ",")

	// Auto-discovery if spec is not explicitly specified and JS bundle is not provided
	if inputSpec == "" && jsFlag == "" {
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
				inputSpec = latestFile
				fmt.Fprintf(stdout, "💡 Auto-detected upstream spec: %s\n", latestFile)
			}
		}
	}

	if inputSpec == "" && jsFlag == "" {
		return errors.New(
			"vortex spec import: spec file or -js flag is required (e.g. `vortex spec import session.har` or `vortex spec import -js=\"*.js\"`)",
		)
	}

	rt, _ := base.NewRuntime("")
	if ct, err := rt.ResolveContract(targetOut); err == nil && ct != nil {
		targetOut = ct.AbsPath
		if targetPkg == "api" && ct.Package != "" {
			targetPkg = ct.Package
		}

		if targetService == "API" && ct.Service != "" {
			targetService = ct.Service
		}
	}

	if targetOut != "-" && filepath.Ext(targetOut) == "" {
		targetOut += ".go"
	}

	if targetPkg == "api" && targetOut != "" {
		dir := filepath.Dir(targetOut)
		if dir != "" && dir != "." {
			baseDir := filepath.Base(dir)
			if baseDir != "" && baseDir != "." && baseDir != "pkg" &&
				baseDir != "internal" && isValidGoPackageName(baseDir) {
				targetPkg = baseDir
			}
		}
	}

	// If input is purely JavaScript bundles
	if inputSpec == "" && jsFlag != "" {
		var jsGlobs []string
		for _, p := range strings.Split(jsFlag, ",") {
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
			return fmt.Errorf("no endpoints discovered in javascript bundles matching %s", jsFlag)
		}

		fmt.Fprintf(
			stdout,
			"Discovered %d RPC endpoints & %d messages from JS bundles\n",
			len(scan.Endpoints),
			len(scan.Messages),
		)

		var existingAPISrc []byte
		if !force {
			if data, readErr := os.ReadFile(targetOut); readErr == nil && len(data) > 0 {
				existingAPISrc = data
			}
		}

		var contractBytes []byte
		if len(existingAPISrc) > 0 {
			doc := jsbundle.ScanToOpenAPI(scan, baseURL)
			engine := openapi.NewMergeEngine()

			mergedBytes, summary, mErr := engine.ReconcileService(existingAPISrc, doc, openapi.MergeConfig{
				SpecFile:    "javascript-bundle",
				PackageName: targetPkg,
				ServiceName: targetService,
				Prune:       prune,
				Additive:    add || !prune,
			})
			if mErr != nil {
				return fmt.Errorf("reconciling contract from js: %w", mErr)
			}

			contractBytes = mergedBytes

			if summary != nil {
				fmt.Fprint(stdout, summary.Render(targetOut))
			}
		} else {
			var gErr error

			contractBytes, gErr = jsbundle.GenerateContract(scan, jsbundle.ContractOptions{
				PackageName: targetPkg,
				ServiceName: targetService,
				BaseURL:     baseURL,
				Engine:      "fast",
			})
			if gErr != nil {
				return fmt.Errorf("generating contract from js: %w", gErr)
			}
		}

		if dryRun {
			fmt.Fprintf(stdout, "%s\n", string(contractBytes))
			return nil
		}

		_ = os.MkdirAll(filepath.Dir(targetOut), 0o755)
		if err := os.WriteFile(targetOut, contractBytes, 0o600); err != nil {
			return fmt.Errorf("writing contract %s: %w", targetOut, err)
		}

		fmt.Fprintf(stdout, "✔ Generated/Reconciled Aoni contract in %s (%d bytes)\n", targetOut, len(contractBytes))

		return nil
	}

	var specData []byte
	if inputSpec == "-" {
		var err error

		specData, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	}

	// Cache and sanitize raw HAR captures and extract credentials into vault
	if cacheFlag || moveFlag {
		tp, _ := pipeline.NewTrafficPipeline(rt.RootDir)
		for _, sPath := range specList {
			if strings.HasSuffix(sPath, ".har") {
				_, _, _ = tp.IngestHAR(sPath, nil, moveFlag, true)
			}
		}
	}

	if jsFlag != "" {
		var jsGlobs []string
		for _, p := range strings.Split(jsFlag, ",") {
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

	doc, err := openapi.LoadSpecWithMode(inputSpec, specData, resolvedMode)
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
	if targetOut != "" && targetOut != "-" && !force {
		if data, readErr := os.ReadFile(targetOut); readErr == nil && len(data) > 0 {
			existingSrc = data
		}
	}

	if len(existingSrc) > 0 {
		// Run Semantic AST Merge Engine
		mergeEngine := openapi.NewMergeEngine()
		mCfg := openapi.MergeConfig{
			SpecFile:       inputSpec,
			PackageName:    targetPkg,
			ServiceName:    targetService,
			Prune:          prune,
			Additive:       add,
			Overwrite:      force,
			DryRun:         dryRun,
			Interactive:    interactive,
			SkipDeprecated: skipDeprecated,
			TypeMap:        tm,
		}

		reconciledSrc, summary, mergeErr := mergeEngine.ReconcileService(existingSrc, doc, mCfg)
		if mergeErr != nil {
			return fmt.Errorf("reconciling contract %s: %w", targetOut, mergeErr)
		}

		fmt.Fprint(stdout, summary.Render(targetOut))

		if !dryRun {
			if err := os.WriteFile(targetOut, reconciledSrc, 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", targetOut, err)
			}
		}

		return nil
	}

	cfg := openapi.ImportConfig{
		SpecFile:       inputSpec,
		PackageName:    targetPkg,
		ServiceName:    targetService,
		OutputFile:     targetOut,
		BaseURL:        baseURL,
		SkipDeprecated: skipDeprecated,
		SplitModels:    splitModels,
		IncludePaths:   includePaths,
		ExcludePaths:   excludePaths,
		TypeMap:        tm,
		MergeMode:      resolvedMode,
	}

	if splitModels {
		apiSrc, modelsSrc, err := openapi.GenerateSplitContract(doc, cfg)
		if err != nil {
			return fmt.Errorf("generating split contract: %w", err)
		}

		outDir := filepath.Dir(targetOut)
		if outDir == "" {
			outDir = "."
		}

		apiPath := targetOut
		modelsPath := filepath.Join(outDir, "models.go")

		if !dryRun {
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

	if targetOut != "" && targetOut != "-" {
		if !dryRun {
			dir := filepath.Dir(targetOut)
			if dir != "" && dir != "." {
				_ = os.MkdirAll(dir, 0o755)
			}

			if err := os.WriteFile(targetOut, src, 0o600); err != nil {
				return fmt.Errorf("writing output file %s: %w", targetOut, err)
			}

			fmt.Fprintf(stdout, "✔ Generated Aoni contract in %s (%d bytes)\n", targetOut, len(src))
		} else {
			fmt.Fprintf(stdout, "[dry-run] Would generate %s (%d bytes)\n", targetOut, len(src))
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
