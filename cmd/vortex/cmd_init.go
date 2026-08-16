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
)

// CmdInit initializes and scaffolds a .vortex.yml configuration file from discovered contracts.
type CmdInit struct{}

func (c *CmdInit) Name() string      { return "init" }
func (c *CmdInit) Aliases() []string { return []string{"scaffold"} }
func (c *CmdInit) Synopsis() string {
	return "Scaffold .vortex.yml workspace configuration from auto-discovered contracts"
}
func (c *CmdInit) Usage() string { return "vortex init [-force] [-dir=.]" }

func (c *CmdInit) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		forceFlag        = fs.Bool("force", false, "Overwrite existing configuration/contract files")
		forceF           = fs.Bool("f", false, "Alias for --force")
		dirFlag          = fs.String("dir", "", "Target workspace directory (default: current repository root)")
		fromOpenAPIFlag  = fs.String("from-openapi", "", "Path or URL to OpenAPI/Swagger specification to import")
		fromHARFlag      = fs.String("from-har", "", "Path to W3C HAR 1.2 network traffic recording to import")
		fromAsyncAPIFlag = fs.String("from-asyncapi", "", "Path or URL to AsyncAPI 2.x/3.x specification to import")
		pkgFlag          = fs.String("pkg", "", "Go package name when importing from specifications")
		serviceFlag      = fs.String("service", "", "Service interface name when importing from specifications")
		outFlag          = fs.String("out", "", "Destination file for imported contract (default: api.go)")
		excludeFlag      = fs.String("exclude", "", "Comma-separated path patterns or globs to ignore during discovery")
		matchFlag        = fs.String("match", "", "Path pattern or glob to restrict contract discovery")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex init — Initialize .vortex.yml Workspace or Scaffold from Specs\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex init [flags]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(
			stderr,
			"  vortex init                                     # Discover repository & create .vortex.yml\n",
		)
		fmt.Fprintf(stderr, "  vortex init -from-openapi=swagger.json          # Scaffold contract from OpenAPI\n")
		fmt.Fprintf(
			stderr,
			"  vortex init -from-har=traffic.har -pkg=api      # Ingest recorded traffic into Go contract\n",
		)
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *fromHARFlag != "" && *fromOpenAPIFlag == "" {
		*fromOpenAPIFlag = *fromHARFlag
	}

	targetDir := *dirFlag
	if targetDir == "" && len(fs.Args()) > 0 {
		targetDir = fs.Arg(0)
	}

	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	force := *forceFlag || *forceF

	if *fromOpenAPIFlag != "" {
		pkgName := *pkgFlag
		if pkgName == "" {
			pkgName = filepath.Base(targetDir)
			if pkgName == "." || pkgName == "/" || pkgName == "\\" {
				pkgName = "api"
			}
		}

		outPath := *outFlag
		if outPath == "" {
			outPath = filepath.Join(targetDir, "api.go")
		} else if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(targetDir, outPath)
		}

		specPath := *fromOpenAPIFlag
		if !filepath.IsAbs(specPath) {
			if _, err := os.Stat(specPath); err != nil {
				cand := filepath.Join(targetDir, specPath)
				if _, sErr := os.Stat(cand); sErr == nil {
					specPath = cand
				}
			}
		}

		importCfg := openapi.ImportConfig{
			SpecFile:    specPath,
			PackageName: pkgName,
			ServiceName: *serviceFlag,
			OutputFile:  outPath,
		}

		res, impErr := openapi.Import(importCfg)
		if impErr != nil {
			return fmt.Errorf("importing openapi spec: %w", impErr)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}

		if err := os.WriteFile(outPath, res.ContractCode, 0o600); err != nil {
			return fmt.Errorf("writing contract file: %w", err)
		}

		fmt.Fprintf(stdout, "✔ Successfully imported OpenAPI contract: %s\n", outPath)
		fmt.Fprintf(stdout, "  • Services: %d\n", res.ServicesCount)
		fmt.Fprintf(stdout, "  • Methods:  %d\n", res.MethodsCount)
		fmt.Fprintf(stdout, "  • Structs:  %d\n\n", res.StructsCount)
	}

	if *fromAsyncAPIFlag != "" {
		pkgName := *pkgFlag
		if pkgName == "" {
			pkgName = filepath.Base(targetDir)
			if pkgName == "." || pkgName == "/" || pkgName == "\\" {
				pkgName = "api"
			}
		}

		outPath := *outFlag
		if outPath == "" {
			outPath = filepath.Join(targetDir, "api.go")
		} else if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(targetDir, outPath)
		}

		importCfg := asyncapi.ImportConfig{
			SpecFile:    *fromAsyncAPIFlag,
			PackageName: pkgName,
			ServiceName: *serviceFlag,
			OutputFile:  outPath,
		}

		res, impErr := asyncapi.Import(importCfg)
		if impErr != nil {
			return fmt.Errorf("importing asyncapi spec: %w", impErr)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}

		if err := os.WriteFile(outPath, res.ContractCode, 0o600); err != nil {
			return fmt.Errorf("writing contract file: %w", err)
		}

		fmt.Fprintf(stdout, "✔ Successfully imported AsyncAPI contract: %s\n", outPath)
		fmt.Fprintf(stdout, "  • Services: %d\n", res.ServicesCount)
		fmt.Fprintf(stdout, "  • Methods:  %d\n", res.MethodsCount)
		fmt.Fprintf(stdout, "  • Structs:  %d\n\n", res.StructsCount)
	}

	var discOpts project.AutoDiscoverOptions
	if *excludeFlag != "" {
		discOpts.Exclude = strings.Split(*excludeFlag, ",")
	}

	discOpts.Match = *matchFlag

	cfg, err := project.Init(targetDir, force, discOpts)
	if err != nil {
		return fmt.Errorf("initializing workspace: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Created %s (%d service(s) configured)\n", cfg.ConfigPath, len(cfg.Contracts))

	for _, ct := range cfg.Contracts {
		fmt.Fprintf(stdout, "  ↳ %-12s -> %s\n", ct.Name, ct.File)
	}

	fmt.Fprintf(stdout, "\nReady: Run `vortex status` to inspect workspace health.\n")

	return nil
}
