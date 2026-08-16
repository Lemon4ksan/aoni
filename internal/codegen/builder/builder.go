// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package builder provides high-level programmatic compilation and file-watching routines for Vortex AST code generation.
package builder

import (
	"context"
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

// Config configures the compilation and output generation behavior of [Builder].
type Config struct {
	// OutFlag specifies an explicit destination file path (used primarily for single-file builds).
	OutFlag string
	// PkgFlag overrides the generated Go package name.
	PkgFlag string
	// Verbose enables detailed diagnostics and skip notices.
	Verbose bool
	// DryRun generates code in memory without writing to disk.
	DryRun bool
	// HarnessFlag enables emission of load and benchmark harness (api_harness.gen.go).
	HarnessFlag bool
}

// Result captures the outcome of a single source file compilation.
type Result struct {
	SourceFile    string
	OutputFile    string
	ServicesCount int
	StructsCount  int
	TuplesCount   int
	BitpacksCount int
	UnionsCount   int
	BytesCount    int
	Skipped       bool
	Code          []byte
}

// Builder orchestrates AST parsing, static analysis, optimization, and emission passes.
type Builder struct {
	cfg       Config
	parser    *parser.Parser
	analyzer  *analysis.Analyzer
	optimizer *optimizer.Optimizer
	emitter   *emitter.Emitter
}

// New creates a new [Builder] configured with the provided [Config].
func New(cfg Config) *Builder {
	return &Builder{
		cfg:       cfg,
		parser:    parser.NewParser(),
		analyzer:  analysis.NewAnalyzer(),
		optimizer: optimizer.NewOptimizer(),
		emitter:   emitter.NewEmitter(),
	}
}

// BuildFile parses, analyzes, optimizes, and compiles a single Go source file.
func (b *Builder) BuildFile(ctx context.Context, srcFile, outFile string) (*Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	root, err := b.parser.ParseFile(srcFile)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", srcFile, err)
	}

	if b.cfg.PkgFlag != "" {
		root.PackageName = b.cfg.PkgFlag
	}

	hasTargets := len(root.Services) > 0 || len(root.Tuples) > 0 ||
		len(root.Bitpacks) > 0 || len(root.Unions) > 0

	if !hasTargets {
		for _, st := range root.Structs {
			if st.GenValueEncoder {
				hasTargets = true
				break
			}
		}
	}

	if !hasTargets {
		return &Result{
			SourceFile: srcFile,
			Skipped:    true,
		}, nil
	}

	diags := b.analyzer.Analyze(root)
	hasErrors := false

	for _, d := range diags {
		if d.Severity == analysis.SeverityError {
			hasErrors = true
			break
		}
	}

	if hasErrors {
		return nil, fmt.Errorf("analysis error in %s", srcFile)
	}

	b.optimizer.Optimize(root)

	code, err := b.emitter.Emit(root)
	if err != nil {
		return nil, fmt.Errorf("emit %s: %w", srcFile, err)
	}

	targetOut := outFile
	if targetOut == "" {
		dir := filepath.Dir(srcFile)
		base := filepath.Base(srcFile)
		ext := filepath.Ext(base)
		targetOut = filepath.Join(dir, strings.TrimSuffix(base, ext)+".gen.go")
	}

	if !b.cfg.DryRun {
		if err := os.WriteFile(targetOut, code, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", targetOut, err)
		}
	}

	if b.cfg.HarnessFlag && len(root.Services) > 0 {
		harnessDir := filepath.Dir(srcFile)
		harnessBase := filepath.Base(srcFile)
		harnessExt := filepath.Ext(harnessBase)
		harnessTarget := filepath.Join(harnessDir, strings.TrimSuffix(harnessBase, harnessExt)+"_harness.gen.go")
		_, _ = b.BuildHarness(ctx, srcFile, harnessTarget)
	}

	return &Result{
		SourceFile:    srcFile,
		OutputFile:    targetOut,
		ServicesCount: len(root.Services),
		StructsCount:  len(root.Structs),
		TuplesCount:   len(root.Tuples),
		BitpacksCount: len(root.Bitpacks),
		UnionsCount:   len(root.Unions),
		BytesCount:    len(code),
		Code:          code,
	}, nil
}

// BuildHarness parses, analyzes, optimizes, and compiles a test/bench harness for a Go source file.
func (b *Builder) BuildHarness(ctx context.Context, srcFile, outFile string) (*Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	root, err := b.parser.ParseFile(srcFile)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", srcFile, err)
	}

	if len(root.Services) == 0 {
		return &Result{
			SourceFile: srcFile,
			Skipped:    true,
		}, nil
	}

	code, err := b.emitter.EmitHarness(root)
	if err != nil {
		return nil, fmt.Errorf("emit harness for %s: %w", srcFile, err)
	}

	targetOut := outFile
	if targetOut == "" {
		dir := filepath.Dir(srcFile)
		base := filepath.Base(srcFile)
		ext := filepath.Ext(base)
		targetOut = filepath.Join(dir, strings.TrimSuffix(base, ext)+"_harness.gen.go")
	}

	if !b.cfg.DryRun {
		if err := os.WriteFile(targetOut, code, 0o600); err != nil {
			return nil, fmt.Errorf("write harness %s: %w", targetOut, err)
		}

		testCode, tErr := b.emitter.EmitHarnessTests(root)
		if tErr == nil && len(testCode) > 0 {
			testOut := strings.TrimSuffix(targetOut, ".gen.go") + "_test.go"
			if strings.HasSuffix(targetOut, ".go") && !strings.HasSuffix(targetOut, ".gen.go") {
				testOut = strings.TrimSuffix(targetOut, ".go") + "_test.go"
			}

			_ = os.WriteFile(testOut, testCode, 0o600)
		}
	}

	return &Result{
		SourceFile:    srcFile,
		OutputFile:    targetOut,
		ServicesCount: len(root.Services),
		BytesCount:    len(code),
		Code:          code,
	}, nil
}

// BuildFiles compiles a slice of Go source files in sequence.
func (b *Builder) BuildFiles(ctx context.Context, files []string) ([]*Result, error) {
	var (
		results  = make([]*Result, 0, len(files))
		errCount int
		lastErr  error
	)

	for _, file := range files {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		out := b.cfg.OutFlag
		if out == "" || len(files) > 1 {
			dir := filepath.Dir(file)
			base := filepath.Base(file)
			ext := filepath.Ext(base)
			out = filepath.Join(dir, strings.TrimSuffix(base, ext)+".gen.go")
		}

		res, err := b.BuildFile(ctx, file, out)
		if err != nil {
			errCount++
			lastErr = err

			continue
		}

		results = append(results, res)
	}

	if errCount > 0 {
		return results, fmt.Errorf("compilation failed for %d file(s) (last error: %w)", errCount, lastErr)
	}

	return results, nil
}

// Watch continuously monitors the provided files for filesystem modifications and triggers onChange.
func (b *Builder) Watch(ctx context.Context, files []string, onChange func(file string, res *Result, err error)) error {
	modTimes := make(map[string]time.Time, len(files))
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			modTimes[f] = fi.ModTime()
		}
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for _, f := range files {
				fi, err := os.Stat(f)
				if err != nil {
					continue
				}

				lastTime := modTimes[f]
				if fi.ModTime().After(lastTime) {
					modTimes[f] = fi.ModTime()

					out := b.cfg.OutFlag
					if out == "" || len(files) > 1 {
						dir := filepath.Dir(f)
						base := filepath.Base(f)
						ext := filepath.Ext(base)
						out = filepath.Join(dir, strings.TrimSuffix(base, ext)+".gen.go")
					}

					res, bErr := b.BuildFile(ctx, f, out)
					if onChange != nil {
						onChange(f, res, bErr)
					}
				}
			}
		}
	}
}

// CollectInputFiles resolves input file paths from flags, environment variables, or path patterns (e.g. ./...).
func CollectInputFiles(fileFlag string, args []string) []string {
	if fileFlag != "" {
		return []string{fileFlag}
	}

	if gofile := os.Getenv("GOFILE"); gofile != "" && len(args) == 0 {
		return []string{gofile}
	}

	if len(args) == 0 {
		args = []string{"./..."}
	}

	var matched []string

	seen := make(map[string]bool)

	for _, target := range args {
		target = filepath.Clean(target)

		if strings.HasSuffix(target, "/...") || target == "./..." || strings.HasSuffix(target, `\...`) ||
			target == "..." || target == "." {
			baseDir := target
			baseDir = strings.TrimSuffix(baseDir, "/...")
			baseDir = strings.TrimSuffix(baseDir, `\...`)
			baseDir = strings.TrimSuffix(baseDir, "...")

			if baseDir == "" || baseDir == "." {
				baseDir = "."
			}

			_ = filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d == nil {
					return err
				}

				if d.IsDir() {
					name := d.Name()
					if name == ".git" || name == "vendor" || name == "node_modules" || name == ".system_generated" {
						return filepath.SkipDir
					}

					return nil
				}

				if IsEligibleGoFile(path) && !seen[path] {
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
			_ = filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d == nil {
					return err
				}

				if d.IsDir() {
					name := d.Name()
					if name == ".git" || name == "vendor" || name == "node_modules" || name == ".system_generated" {
						return filepath.SkipDir
					}

					return nil
				}

				if IsEligibleGoFile(path) && !seen[path] {
					seen[path] = true
					matched = append(matched, path)
				}

				return nil
			})

			continue
		}

		if IsEligibleGoFile(target) && !seen[target] {
			seen[target] = true
			matched = append(matched, target)
		}
	}

	return matched
}

// IsEligibleGoFile checks whether a file path is a candidate for code generation (non-test, non-generated .go file).
func IsEligibleGoFile(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}

	if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".gen.go") {
		return false
	}

	return true
}
