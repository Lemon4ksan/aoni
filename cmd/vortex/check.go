// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/lint"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func runCheck(args []string) {
	checkCmd := flag.NewFlagSet("check", flag.ExitOnError)
	fixFlag := checkCmd.Bool("fix", false, "Automatically apply safe, non-destructive fixes")
	jsonFlag := checkCmd.Bool("json", false, "Output diagnostics as JSON")
	disableFlag := checkCmd.String("disable", "", "Comma-separated list of rule IDs/names to disable")
	enableFlag := checkCmd.String("enable", "", "Comma-separated list of rule IDs/names to enable")
	strictFlag := checkCmd.Bool("strict", false, "Treat warnings as errors (exit 1)")

	_ = checkCmd.Parse(args)
	files := collectInputFiles("", checkCmd.Args())

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "vortex check: no Go source files found to inspect\n")
		os.Exit(1)
	}

	reg := lint.DefaultRegistry()
	if *disableFlag != "" {
		reg.Disable(strings.Split(*disableFlag, ",")...)
	}

	if *enableFlag != "" {
		reg.Enable(strings.Split(*enableFlag, ",")...)
	}

	engine := lint.NewEngine(reg)
	p := codeparser.NewParser()
	fset := token.NewFileSet()

	var reports []*lint.Report

	for _, file := range files {
		if abs, err := filepath.Abs(file); err == nil {
			file = abs
		}

		srcBytes, err := os.ReadFile(file)
		if err != nil {
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

		hasTargets := len(root.Services) > 0 || len(root.Tuples) > 0 || len(root.UnrecognizedDirectives) > 0
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
			Ignores:     lint.ParseIgnores(fset, astFile),
		}

		report, err := engine.Run(pass)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vortex check: error checking %s: %v\n", file, err)
			continue
		}

		reports = append(reports, report)
	}

	merged := lint.MergeReports(reports...)

	// Auto-fix execution if requested
	if *fixFlag && merged.FixableCount() > 0 {
		applied, err := merged.ApplyFixes()
		if err != nil {
			fmt.Fprintf(os.Stderr, "vortex check: error applying fixes: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "✔ Successfully applied %d safe automated fix(es)\n\n", applied)
		}

		var remaining []lint.Diagnostic
		for _, d := range merged.Diagnostics {
			if d.Fix == nil {
				remaining = append(remaining, d)
			}
		}

		merged.Diagnostics = remaining
	}

	// Output
	if *jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(merged)
	} else {
		targetSummary := "./..."
		if len(files) == 1 {
			targetSummary = files[0]
		}

		lint.FormatReport(os.Stdout, targetSummary, merged)
	}

	if merged.Errors() > 0 || (*strictFlag && merged.Warnings() > 0) {
		os.Exit(1)
	}
}
