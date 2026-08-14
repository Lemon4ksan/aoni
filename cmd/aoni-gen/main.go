// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// aoni-gen is an AST-driven code generator that turns declarative Go API interfaces into
// zero-allocation, type-safe aoni network clients.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func main() {
	var (
		fileFlag = flag.String(
			"file",
			"",
			"Path to the input Go source file containing @aoni directives (or set via $GOFILE)",
		)
		outFlag     = flag.String("out", "", "Path to output generated .gen.go file (default: <filename>.gen.go)")
		pkgFlag     = flag.String("pkg", "", "Override package name in generated code")
		verboseFlag = flag.Bool("v", false, "Enable verbose compilation logging")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of aoni-gen:\n")
		fmt.Fprintf(os.Stderr, "  aoni-gen [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  //go:generate go run github.com/lemon4ksan/aoni/cmd/aoni-gen -file=$GOFILE\n")
	}

	flag.Parse()

	inputFile := *fileFlag
	if inputFile == "" {
		inputFile = os.Getenv("GOFILE")
	}

	if inputFile == "" {
		fmt.Fprintf(os.Stderr, "aoni-gen: missing input file. Specify -file or run via go generate ($GOFILE)\n")
		flag.Usage()
		os.Exit(1)
	}

	if *verboseFlag {
		fmt.Printf("aoni-gen: parsing source file: %s\n", inputFile)
	}

	// 1. Parse Source
	p := parser.NewParser()

	root, err := p.ParseFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen: parsing error: %v\n", err)
		os.Exit(1)
	}

	if *pkgFlag != "" {
		root.PackageName = *pkgFlag
	}

	if len(root.Services) == 0 && len(root.Structs) == 0 && len(root.Tuples) == 0 {
		fmt.Fprintf(os.Stderr, "aoni-gen: no @aoni:service or @aoni:dto definitions found in %s\n", inputFile)
		os.Exit(0)
	}

	// 2. Semantic Analysis
	analyzer := analysis.NewAnalyzer()
	diags := analyzer.Analyze(root)

	hasErrors := false
	for _, d := range diags {
		if d.Severity == analysis.SeverityError {
			hasErrors = true

			fmt.Fprintf(os.Stderr, "aoni-gen: error: %s\n", d)
		} else if *verboseFlag {
			fmt.Printf("aoni-gen: warning: %s\n", d)
		}
	}

	if hasErrors {
		fmt.Fprintf(os.Stderr, "aoni-gen: compilation aborted due to semantic errors\n")
		os.Exit(1)
	}

	// 3. Optimize IR
	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	// 4. Emit Go Code
	em := emitter.NewEmitter()

	code, err := em.Emit(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen: emission error: %v\n", err)
		os.Exit(1)
	}

	// 5. Write Output File
	outputFile := *outFlag
	if outputFile == "" {
		dir := filepath.Dir(inputFile)
		base := filepath.Base(inputFile)
		ext := filepath.Ext(base)
		nameWithoutExt := strings.TrimSuffix(base, ext)
		outputFile = filepath.Join(dir, nameWithoutExt+".gen.go")
	}

	if err := os.WriteFile(outputFile, code, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen: failed to write output file %s: %v\n", outputFile, err)
		os.Exit(1)
	}

	if *verboseFlag {
		fmt.Printf("aoni-gen: generated %s successfully (%d bytes)\n", outputFile, len(code))
	}
}
