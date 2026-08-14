// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// aoni-gen is an AST-driven code generator that turns declarative Go API interfaces into
// zero-allocation, type-safe aoni network clients.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		watchFlag   = flag.Bool("watch", false, "Watch source directories and rebuild on file modification")
		verboseFlag = flag.Bool("v", false, "Enable verbose compilation logging")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "aoni-gen — Ultra-high-performance RPC/HTTP client code generator for Go\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  aoni-gen [flags] [packages/files...]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  aoni-gen ./...\n")
		fmt.Fprintf(os.Stderr, "  aoni-gen -file=market.go\n")
		fmt.Fprintf(os.Stderr, "  aoni-gen -watch ./pkg/...\n")
		fmt.Fprintf(os.Stderr, "  //go:generate go run github.com/lemon4ksan/aoni/cmd/aoni-gen -file=$GOFILE\n")
	}

	flag.Parse()

	files := collectInputFiles(*fileFlag, flag.Args())
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "aoni-gen: no input files found. Specify package paths (e.g. ./...) or -file=$GOFILE\n")
		flag.Usage()
		os.Exit(1)
	}

	// 1. Initial build
	buildAll(files, *outFlag, *pkgFlag, *verboseFlag)

	// 2. Watch mode loop
	if *watchFlag {
		fmt.Printf("\n[aoni-gen] Watching for file changes across %d files... (Press Ctrl+C to stop)\n", len(files))
		watchLoop(files, *outFlag, *pkgFlag, *verboseFlag)
	}
}

func collectInputFiles(fileFlag string, args []string) []string {
	if fileFlag != "" {
		return []string{fileFlag}
	}

	if gofile := os.Getenv("GOFILE"); gofile != "" && len(args) == 0 {
		return []string{gofile}
	}

	if len(args) == 0 {
		args = []string{"."}
	}

	var matched []string

	seen := make(map[string]bool)

	for _, target := range args {
		if strings.HasSuffix(target, "/...") || target == "./..." || strings.HasSuffix(target, `\...`) {
			baseDir := strings.TrimSuffix(strings.TrimSuffix(target, "/..."), `\...`)
			if baseDir == "" || baseDir == "." {
				baseDir = "."
			}

			_ = filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}

				if d == nil {
					return nil
				}

				if d.IsDir() {
					name := d.Name()
					if name == ".git" || name == "vendor" || name == "node_modules" {
						return filepath.SkipDir
					}

					return nil
				}

				if isEligibleGoFile(path) && !seen[path] {
					seen[path] = true
					matched = append(matched, path)
				}

				return nil
			})

			continue
		}

		fi, err := os.Stat(target)
		if err != nil {
			continue
		}

		if fi.IsDir() {
			entries, _ := os.ReadDir(target)
			for _, e := range entries {
				if !e.IsDir() && isEligibleGoFile(e.Name()) {
					p := filepath.Join(target, e.Name())
					if !seen[p] {
						seen[p] = true
						matched = append(matched, p)
					}
				}
			}

			continue
		}

		if isEligibleGoFile(target) && !seen[target] {
			seen[target] = true
			matched = append(matched, target)
		}
	}

	return matched
}

func isEligibleGoFile(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}

	if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".gen.go") {
		return false
	}

	return true
}

func buildAll(files []string, outFlag, pkgFlag string, verbose bool) {
	for _, file := range files {
		out := outFlag
		if out == "" {
			dir := filepath.Dir(file)
			base := filepath.Base(file)
			ext := filepath.Ext(base)
			out = filepath.Join(dir, strings.TrimSuffix(base, ext)+".gen.go")
		}

		compileFile(file, out, pkgFlag, verbose)
	}
}

func compileFile(inputFile, outputFile, pkgFlag string, verbose bool) {
	p := parser.NewParser()

	root, err := p.ParseFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen: error parsing %s: %v\n", inputFile, err)
		return
	}

	if pkgFlag != "" {
		root.PackageName = pkgFlag
	}

	if len(root.Services) == 0 && len(root.Structs) == 0 && len(root.Tuples) == 0 {
		if verbose {
			fmt.Printf("aoni-gen: skipping %s (no @aoni directives found)\n", inputFile)
		}

		return
	}

	analyzer := analysis.NewAnalyzer()
	diags := analyzer.Analyze(root)

	hasErrors := false
	for _, d := range diags {
		if d.Severity == analysis.SeverityError {
			hasErrors = true

			fmt.Fprintf(os.Stderr, "aoni-gen: [ERROR] %s: %s\n", d.Target, d.Message)
		} else if verbose {
			fmt.Printf("aoni-gen: [WARN] %s: %s\n", d.Target, d.Message)
		}
	}

	if hasErrors {
		fmt.Fprintf(os.Stderr, "aoni-gen: compilation aborted for %s\n", inputFile)
		return
	}

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	em := emitter.NewEmitter()

	code, err := em.Emit(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen: emission error for %s: %v\n", inputFile, err)
		return
	}

	if err := os.WriteFile(outputFile, code, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "aoni-gen: failed writing %s: %v\n", outputFile, err)
		return
	}

	fmt.Printf("✔ Generated %s (%d bytes, %d service(s), %d dto(s))\n",
		outputFile, len(code), len(root.Services), len(root.Structs))
}

func watchLoop(files []string, outFlag, pkgFlag string, verbose bool) {
	modTimes := make(map[string]time.Time)
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			modTimes[f] = fi.ModTime()
		}
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		for _, f := range files {
			fi, err := os.Stat(f)
			if err != nil {
				continue
			}

			lastTime := modTimes[f]
			if fi.ModTime().After(lastTime) {
				modTimes[f] = fi.ModTime()
				fmt.Printf("\n[%s] Change detected in %s:\n", time.Now().Format("15:04:05"), f)

				out := outFlag
				if out == "" {
					dir := filepath.Dir(f)
					base := filepath.Base(f)
					ext := filepath.Ext(base)
					out = filepath.Join(dir, strings.TrimSuffix(base, ext)+".gen.go")
				}

				compileFile(f, out, pkgFlag, verbose)
			}
		}
	}
}
