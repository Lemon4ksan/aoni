// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/cache"
	"github.com/lemon4ksan/aoni/internal/codegen/lint"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdCheck performs static contract validation and applies automated fixes.
type CmdCheck struct{}

func (c *CmdCheck) Name() string      { return "check" }
func (c *CmdCheck) Aliases() []string { return []string{"lint", "inspect"} }
func (c *CmdCheck) Synopsis() string {
	return "Static contract linter and diagnostic inspector (supports --fix)"
}
func (c *CmdCheck) Usage() string { return "vortex check [flags] [packages/files...]" }

func (c *CmdCheck) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fixFlag    = fs.Bool("fix", false, "Automatically apply safe, non-destructive fixes")
		jsonFlag   = fs.Bool("json", false, "Output diagnostics as JSON (shorthand for -format=json)")
		formatFlag = fs.String(
			"format",
			"terminal",
			"Output format: terminal, json, github (GitHub Actions annotations), sarif",
		)
		disableFlag  = fs.String("disable", "", "Comma-separated list of rule IDs/names to disable")
		enableFlag   = fs.String("enable", "", "Comma-separated list of rule IDs/names to enable")
		strictFlag   = fs.Bool("strict", false, "Treat warnings as errors")
		noCacheFlag  = fs.Bool("no-cache", false, "Disable incremental validation cache")
		dirFlag      = fs.String("dir", "", "Target workspace directory (default: current root)")
		maxDepthFlag = fs.Int("max-depth", 6, "Maximum directory search depth (0 for unlimited)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex check — Static Contract Linter & Diagnostic Inspector\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(
			stderr,
			"  vortex check [-fix] [-format=terminal|json|github|sarif] [-max-depth=6] [-disable=rules] [-enable=rules] [-strict] [paths...]\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-dir" || arg == "-format" || arg == "-disable" || arg == "-enable" || arg == "-max-depth") &&
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

	files := builder.CollectInputFiles("", fs.Args(), builder.CollectOptions{
		MaxDepth: *maxDepthFlag,
	})
	if len(files) == 0 {
		return fmt.Errorf(
			"no Go source files found to inspect (searched up to depth %d). Use -max-depth=10 or specify file paths",
			*maxDepthFlag,
		)
	}

	reg := lint.DefaultRegistry()

	rootDir := *dirFlag
	if rootDir == "" {
		cwd, _ := os.Getwd()
		rootDir, _, _ = project.FindRoot(cwd)
	}

	if cfg, _ := project.Load(rootDir); cfg != nil {
		reg.Disable(cfg.AllIgnoredRules()...)
	}

	if *disableFlag != "" {
		reg.Disable(strings.Split(*disableFlag, ",")...)
	}

	if *enableFlag != "" {
		reg.Enable(strings.Split(*enableFlag, ",")...)
	}

	engine := lint.NewEngine(reg)
	p := codeparser.NewParser()
	fset := token.NewFileSet()

	lintCache, _ := cache.LoadLintCache(rootDir)

	var reports []*lint.Report

	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if abs, err := filepath.Abs(file); err == nil {
			file = abs
		}

		srcBytes, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		relFile, _ := filepath.Rel(rootDir, file)

		// Fast cache check
		if !*noCacheFlag && !*fixFlag && lintCache.IsFresh(relFile, srcBytes) {
			continue
		}

		astFile, err := parser.ParseFile(fset, file, srcBytes, parser.ParseComments)
		if err != nil {
			continue
		}

		root, err := p.ParseFile(file)
		if err != nil {
			continue
		}

		hasTargets := len(root.Services) > 0 || len(root.Tuples) > 0 || len(root.Bitpacks) > 0 ||
			len(root.UnrecognizedDirectives) > 0 || len(root.Unions) > 0

		if !hasTargets {
			for _, st := range root.Structs {
				if st.GenValueEncoder {
					hasTargets = true
					break
				}
			}
		}

		if !hasTargets {
			continue
		}

		pass := &lint.Pass{
			RootIR:      root,
			FileSet:     fset,
			ASTFile:     astFile,
			SourceBytes: srcBytes,
			FilePath:    file,
			RootDir:     rootDir,
			Ignores:     lint.ParseIgnores(fset, astFile),
		}

		report, err := engine.Run(pass)
		if err != nil {
			fmt.Fprintf(stderr, "vortex check: error checking %s: %v\n", file, err)
			continue
		}

		if !*noCacheFlag {
			lintCache.Put(relFile, srcBytes, len(report.Diagnostics))
		}

		reports = append(reports, report)
	}

	if !*noCacheFlag && !*fixFlag {
		_ = lintCache.Save(rootDir)
	}

	merged := lint.MergeReports(reports...)

	if *fixFlag && merged.FixableCount() > 0 {
		applied, err := merged.ApplyFixes()
		if err != nil {
			fmt.Fprintf(stderr, "vortex check: error applying fixes: %v\n", err)
		} else {
			fmt.Fprintf(stdout, "✔ Successfully applied %d safe automated fix(es)\n\n", applied)
		}

		var remaining []lint.Diagnostic
		for _, d := range merged.Diagnostics {
			if d.Fix == nil {
				remaining = append(remaining, d)
			}
		}

		merged.Diagnostics = remaining
	}

	switch strings.ToLower(*formatFlag) {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(merged)
	case "github", "github-actions", "gha":
		lint.FormatGitHubActions(stdout, merged)
	case "sarif":
		_ = lint.FormatSARIF(stdout, merged)
	default:
		if *jsonFlag {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(merged)
		} else {
			targetSummary := "./..."
			if len(files) == 1 {
				targetSummary = files[0]
			}

			lint.FormatReport(stdout, targetSummary, merged)
		}
	}

	if merged.Errors() > 0 || (*strictFlag && merged.Warnings() > 0) {
		return fmt.Errorf("contract check failed with %d error(s), %d warning(s)", merged.Errors(), merged.Warnings())
	}

	return nil
}
