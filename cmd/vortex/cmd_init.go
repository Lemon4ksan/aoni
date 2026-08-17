// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/asyncapi"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
	"github.com/lemon4ksan/aoni/internal/codegen/spec"
)

// CmdInit initializes a .vortex.yml workspace or scaffolds a new API package from templates/specs.
type CmdInit struct{}

func (c *CmdInit) Name() string      { return "init" }
func (c *CmdInit) Aliases() []string { return []string{"scaffold", "new", "create"} }
func (c *CmdInit) Synopsis() string {
	return "Scaffold a new API package, contract template, or initialize .vortex.yml workspace"
}

func (c *CmdInit) Usage() string {
	return "vortex init [package-name|path] [-tpl=<kind>] [-from=<spec.json|har>] [flags]"
}

func (c *CmdInit) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		forceFlag    = fs.Bool("force", false, "Overwrite existing configuration/contract files")
		forceF       = fs.Bool("f", false, "Alias for --force")
		dirFlag      = fs.String("dir", "", "Target workspace directory (default: current repository root)")
		templateFlag = fs.String(
			"template",
			"",
			"Contract starter template (rest, stream, ws, auth, stealth, grpc, socket, list)",
		)
		templateTpl      = fs.String("tpl", "", "Alias for --template")
		fromFlag         = fs.String("from", "", "Path or URL to OpenAPI, Swagger, or HAR specification to import")
		fromOpenAPIFlag  = fs.String("from-openapi", "", "Alias for --from")
		fromHARFlag      = fs.String("from-har", "", "Alias for --from")
		fromAsyncAPIFlag = fs.String("from-asyncapi", "", "Path or URL to AsyncAPI 2.x/3.x specification to import")
		pkgFlag          = fs.String("pkg", "", "Explicit Go package name")
		serviceFlag      = fs.String("service", "", "Explicit service interface name (default: PascalCase(pkg) + API)")
		baseURLFlag      = fs.String("base-url", "", "Base URL for the generated service contract")
		outFlag          = fs.String("out", "", "Explicit destination file for generated contract")
		excludeFlag      = fs.String("exclude", "", "Comma-separated path patterns or globs to ignore during discovery")
		matchFlag        = fs.String("match", "", "Path pattern or glob to restrict contract discovery")
		mergeModeFlag    = fs.String(
			"mode",
			"union",
			"Merge strategy when combining multiple specs: union (all endpoints), intersect (only matching/common endpoints), diff (only endpoints in first spec)",
		)
		modeFlag = fs.String("merge-mode", "union", "Alias for --mode")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex init — Scaffold New API Package or Initialize Workspace\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex init [pkg-name|path] [flags]             # Scaffold new API package\n")
		fmt.Fprintf(stderr, "  vortex init [pkg-name] -tpl=<kind>              # Scaffold from starter template\n")
		fmt.Fprintf(stderr, "  vortex init [pkg-name] -from=<spec.json|har>    # Ingest spec into new package\n")
		fmt.Fprintf(
			stderr,
			"  vortex init                                     # Discover workspace & create .vortex.yml\n\n",
		)
		fmt.Fprintf(stderr, "Starter Templates (-tpl):\n")
		fmt.Fprintf(stderr, "  • rest (default)  - Standard REST CRUD API with DTOs, query filters, and pagination\n")
		fmt.Fprintf(stderr, "  • stream / sse    - Real-Time Server-Sent Events (SSE) & NDJSON streaming client\n")
		fmt.Fprintf(stderr, "  • ws / websocket  - WebSocket bi-directional typed event messaging\n")
		fmt.Fprintf(stderr, "  • auth / oauth2   - Bearer token auto-rotation and HMAC cryptographic request signing\n")
		fmt.Fprintf(
			stderr,
			"  • stealth / scrape- Anti-bot browser impersonation, p0f OS spoofing, and HTML/DOM scraping\n",
		)
		fmt.Fprintf(stderr, "  • grpc / grpc-web - Framed gRPC-Web & Protobuf microservice client\n")
		fmt.Fprintf(stderr, "  • socket          - Persistent high-throughput binary socket facade\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(
			stderr,
			"  vortex init billing                             # Create pkg/billing/api.go (REST CRUD)\n",
		)
		fmt.Fprintf(stderr, "  vortex init chat -tpl=ws                        # Create pkg/chat/api.go (WebSocket)\n")
		fmt.Fprintf(
			stderr,
			"  vortex init ai -tpl=sse                         # Create pkg/ai/api.go (SSE Streaming)\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex init market -tpl=stealth                 # Create pkg/market/api.go (Anti-Bot Scraper)\n",
		)
		fmt.Fprintf(stderr, "  vortex init client -from=traffic.har            # Ingest HAR into pkg/client/api.go\n")
		fmt.Fprintf(stderr, "  vortex init auth -from=auth.json -pkg=auth      # Ingest OpenAPI into pkg/auth/api.go\n")
		fmt.Fprintf(stderr, "  vortex init -tpl=list                           # View full template documentation\n")
	}

	// Reorder flags before non-flags
	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-dir" || arg == "-template" || arg == "-tpl" || arg == "-from" ||
				arg == "-from-openapi" || arg == "-from-har" || arg == "-from-asyncapi" ||
				arg == "-pkg" || arg == "-service" || arg == "-base-url" || arg == "-out" ||
				arg == "-exclude" || arg == "-match" || arg == "-mode" || arg == "-merge-mode" ||
				arg == "--dir" || arg == "--template" || arg == "--tpl" || arg == "--from" ||
				arg == "--from-openapi" || arg == "--from-har" || arg == "--from-asyncapi" ||
				arg == "--pkg" || arg == "--service" || arg == "--base-url" || arg == "--out" ||
				arg == "--exclude" || arg == "--match" || arg == "--mode" || arg == "--merge-mode") &&
				i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			nonFlags = append(nonFlags, arg)
		}
	}

	if err := fs.Parse(append(flags, nonFlags...)); err != nil {
		return err
	}

	var resolvedMode openapi.MergeMode

	modeVal := strings.ToLower(strings.TrimSpace(*mergeModeFlag))
	if modeVal == "union" && *modeFlag != "union" {
		modeVal = strings.ToLower(strings.TrimSpace(*modeFlag))
	}

	switch modeVal {
	case "intersect", "intersection":
		resolvedMode = openapi.MergeModeIntersection
	case "diff", "difference":
		resolvedMode = openapi.MergeModeDifference
	default:
		resolvedMode = openapi.MergeModeUnion
	}

	// Normalize template flag
	tplKind := *templateFlag
	if tplKind == "" {
		tplKind = *templateTpl
	}

	if strings.EqualFold(tplKind, "list") || strings.EqualFold(tplKind, "help") {
		fmt.Fprint(stdout, spec.PrintExampleHelp())
		return nil
	}

	// Normalize from flag
	specSource := *fromFlag
	if specSource == "" {
		specSource = *fromOpenAPIFlag
	}

	if specSource == "" {
		specSource = *fromHARFlag
	}

	// Identify target workspace directory
	targetDir := *dirFlag
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	force := *forceFlag || *forceF
	posArgs := fs.Args()

	// Detect if positional arguments contain a package name and/or a spec file
	var targetPkgOrPath string
	for len(posArgs) > 0 {
		arg := posArgs[0]
		isSpec := false

		for _, token := range strings.Split(arg, ",") {
			token = strings.TrimSpace(token)
			if strings.HasSuffix(token, ".har") || strings.HasSuffix(token, ".json") ||
				strings.HasSuffix(token, ".yaml") || strings.HasSuffix(token, ".yml") {
				isSpec = true

				if specSource == "" {
					specSource = token
				} else {
					specSource += "," + token
				}
			}
		}

		if !isSpec && targetPkgOrPath == "" {
			targetPkgOrPath = arg
		}

		posArgs = posArgs[1:]
	}

	// If targetPkgOrPath is an existing directory or "." -> treat as targetDir for workspace initialization
	if targetPkgOrPath != "" {
		if fi, err := os.Stat(targetPkgOrPath); err == nil && fi.IsDir() {
			targetDir = targetPkgOrPath
			targetPkgOrPath = ""
		} else if targetPkgOrPath == "." || targetPkgOrPath == "./..." {
			targetPkgOrPath = ""
		}
	}

	// If neither package, template, nor from flag was provided, perform workspace discovery and .vortex.yml initialization
	if targetPkgOrPath == "" && specSource == "" && *fromAsyncAPIFlag == "" && *outFlag == "" && tplKind == "" {
		return c.runWorkspaceInit(targetDir, force, *excludeFlag, *matchFlag, stdout)
	}

	// Branch: Scaffold specific API package / contract
	return c.runPackageInit(ctx, targetDir, targetPkgOrPath, tplKind, specSource, *fromAsyncAPIFlag,
		*pkgFlag, *serviceFlag, *baseURLFlag, *outFlag, force, resolvedMode, stdout)
}

func (c *CmdInit) runWorkspaceInit(targetDir string, force bool, exclude, match string, stdout io.Writer) error {
	var discOpts project.AutoDiscoverOptions
	if exclude != "" {
		discOpts.Exclude = strings.Split(exclude, ",")
	}

	discOpts.Match = match

	cfg, err := project.Init(targetDir, force, discOpts)
	if err != nil {
		return fmt.Errorf("initializing workspace: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Created %s (%d contract(s) configured)\n", cfg.ConfigPath, len(cfg.Contracts))

	for _, ct := range cfg.Contracts {
		fmt.Fprintf(stdout, "  ↳ %-14s -> %s\n", ct.Name, ct.File)
	}

	if len(cfg.Contracts) == 0 {
		fmt.Fprintf(stdout, "\n💡 Tip: Scaffold your first API package with:\n")
		fmt.Fprintf(stdout, "   vortex init billing        # REST CRUD API\n")
		fmt.Fprintf(stdout, "   vortex init chat -tpl=ws   # WebSocket Client\n")
		fmt.Fprintf(stdout, "   vortex init -from=spec.json# Ingest OpenAPI/HAR\n")
	} else {
		fmt.Fprintf(stdout, "\nReady: Run `vortex status` or `vortex` (autopilot) to compile clients.\n")
	}

	return nil
}

func (c *CmdInit) runPackageInit(
	_ context.Context,
	targetDir string,
	pkgOrPath string,
	tplKind string,
	specSource string,
	asyncAPISource string,
	explicitPkg string,
	explicitService string,
	baseURL string,
	explicitOut string,
	force bool,
	mode openapi.MergeMode,
	stdout io.Writer,
) error {
	// 1. Resolve package name, destination path, and service name
	var (
		pkgName     = explicitPkg
		serviceName = explicitService
		outPath     = explicitOut
	)

	if pkgOrPath != "" {
		// Handle comma-separated multiple package initialization: e.g. `vortex init billing,market`
		if strings.Contains(pkgOrPath, ",") {
			pkgs := strings.Split(pkgOrPath, ",")
			for _, p := range pkgs {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}

				if err := c.runPackageInit(context.Background(), targetDir, p, tplKind, specSource, asyncAPISource,
					"", "", baseURL, "", force, mode, stdout); err != nil {
					return err
				}
			}

			return nil
		}

		if strings.HasSuffix(pkgOrPath, ".go") {
			outPath = pkgOrPath
			if pkgName == "" {
				pkgName = filepath.Base(filepath.Dir(pkgOrPath))
				if pkgName == "." || pkgName == "/" || pkgName == "\\" {
					pkgName = "api"
				}
			}
		} else {
			if pkgName == "" {
				pkgName = filepath.Base(pkgOrPath)
			}

			if outPath == "" {
				// Pick standard Go folder convention: check if pkg/ exists, or default to pkg/<name>/api.go
				if _, err := os.Stat(
					filepath.Join(targetDir, "internal"),
				); err == nil &&
					!dirExists(filepath.Join(targetDir, "pkg")) {
					outPath = filepath.Join("internal", pkgName, "api.go")
				} else {
					outPath = filepath.Join("pkg", pkgName, "api.go")
				}
			}
		}
	}

	if pkgName == "" {
		pkgName = "api"
	}

	if outPath == "" {
		outPath = "api.go"
	}

	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(targetDir, outPath)
	}

	if serviceName == "" {
		serviceName = toPascalCaseName(pkgName)
		if !strings.HasSuffix(serviceName, "API") && !strings.HasSuffix(serviceName, "Client") &&
			!strings.HasSuffix(serviceName, "Service") {
			serviceName += "API"
		}
	}

	// Guard: check if file already exists
	if !force {
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("target contract %s already exists (use -force to overwrite)", outPath)
		}
	}

	var (
		contractBytes []byte
		metaKind      string
		methodsCount  int
		structsCount  int
		upstreamCfg   *project.UpstreamConfig
	)

	// Branch generation
	switch {
	case specSource != "":
		metaKind = "OpenAPI/HAR"

		var resolvedSpecs []string
		for _, sp := range strings.Split(specSource, ",") {
			sp = strings.TrimSpace(sp)
			if sp == "" {
				continue
			}

			if !filepath.IsAbs(sp) {
				if _, err := os.Stat(sp); err != nil {
					cand := filepath.Join(targetDir, sp)
					if _, sErr := os.Stat(cand); sErr == nil {
						sp = cand
					}
				}
			}

			resolvedSpecs = append(resolvedSpecs, sp)
		}

		joinedSpec := strings.Join(resolvedSpecs, ",")
		importCfg := openapi.ImportConfig{
			SpecFile:    joinedSpec,
			PackageName: pkgName,
			ServiceName: serviceName,
			OutputFile:  outPath,
			BaseURL:     baseURL,
			MergeMode:   mode,
		}

		res, err := openapi.Import(importCfg)
		if err != nil {
			return fmt.Errorf("importing specification %q: %w", joinedSpec, err)
		}

		contractBytes = res.ContractCode
		methodsCount = res.MethodsCount
		structsCount = res.StructsCount
		upstreamCfg = &project.UpstreamConfig{
			Source: specSource,
			Format: "openapi",
		}

	case asyncAPISource != "":
		metaKind = "AsyncAPI"
		importCfg := asyncapi.ImportConfig{
			SpecFile:    asyncAPISource,
			PackageName: pkgName,
			ServiceName: serviceName,
			OutputFile:  outPath,
		}

		res, err := asyncapi.Import(importCfg)
		if err != nil {
			return fmt.Errorf("importing AsyncAPI spec %q: %w", asyncAPISource, err)
		}

		contractBytes = res.ContractCode
		methodsCount = res.MethodsCount
		structsCount = res.StructsCount
		upstreamCfg = &project.UpstreamConfig{
			Source: asyncAPISource,
			Format: "asyncapi",
		}

	default:
		if tplKind == "" {
			tplKind = "rest"
		}

		ex := spec.LookupExample(tplKind)
		if ex == nil {
			return fmt.Errorf(
				"unknown template kind %q (run `vortex init -tpl=list` to see available starters)",
				tplKind,
			)
		}

		rendered, err := spec.RenderTemplate(ex.Kind, pkgName, serviceName, baseURL)
		if err != nil {
			return fmt.Errorf("rendering template: %w", err)
		}

		contractBytes = []byte(rendered)
		metaKind = fmt.Sprintf("Template [%s]", ex.Kind)
		methodsCount = 5
	}

	// 2. Write contract file
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// #nosec G306
	if err := os.WriteFile(outPath, contractBytes, 0o600); err != nil {
		return fmt.Errorf("writing contract file %s: %w", outPath, err)
	}

	relPath, rErr := filepath.Rel(targetDir, outPath)
	if rErr != nil {
		relPath = outPath
	}

	relSlash := filepath.ToSlash(relPath)

	// 3. Auto-register in .vortex.yml if workspace exists or can be configured
	_ = project.RegisterContract(targetDir, project.ContractConfig{
		Name:     serviceName,
		Package:  pkgName,
		File:     relSlash,
		Upstream: upstreamCfg,
	})

	switch {
	case specSource != "":
		fmt.Fprintf(stdout, "✔ Successfully imported OpenAPI contract: %s\n", outPath)
	case asyncAPISource != "":
		fmt.Fprintf(stdout, "✔ Successfully imported AsyncAPI contract: %s\n", outPath)
	default:
		fmt.Fprintf(stdout, "✔ Successfully initialized package %s (%s)\n", pkgName, metaKind)
	}

	fmt.Fprintf(stdout, "  • Contract File: %s\n", relSlash)
	fmt.Fprintf(stdout, "  • Service Name:  %s\n", serviceName)

	if methodsCount > 0 {
		fmt.Fprintf(stdout, "  • Methods:       %d\n", methodsCount)
	}

	if structsCount > 0 {
		fmt.Fprintf(stdout, "  • DTO Structs:   %d\n", structsCount)
	}

	fmt.Fprintf(stdout, "\nNext Steps:\n")
	fmt.Fprintf(stdout, "  ↳ Compile client:    vortex gen %s\n", relSlash)
	fmt.Fprintf(stdout, "  ↳ Generate mock:     vortex mock %s\n", relSlash)
	fmt.Fprintf(stdout, "  ↳ Audit invariants:  vortex check %s\n\n", relSlash)

	return nil
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func toPascalCaseName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "API"
	}

	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == '/' || r == '\\' || r == '.'
	})

	var sb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}

		sb.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}

	res := sb.String()
	if res == "" {
		return "API"
	}

	return res
}
