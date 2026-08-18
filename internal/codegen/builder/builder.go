// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package builder provides high-level programmatic compilation and file-watching routines for Vortex AST code generation.
package builder

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
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
	// FixturesFlag enables loading realistic response fixtures from traffic cache into mock servers.
	FixturesFlag bool
	// RootDir specifies project root directory (defaults to current working directory or Git root).
	RootDir string
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
		len(root.Bitpacks) > 0 || len(root.Unions) > 0 ||
		generic.Any(root.Structs, func(st *ir.StructIR) bool { return st.GenValueEncoder })

	if !hasTargets {
		return &Result{
			SourceFile: srcFile,
			Skipped:    true,
		}, nil
	}

	diags := b.analyzer.Analyze(root)

	errMsgs := generic.Map(
		generic.Filter(diags, func(d analysis.Diagnostic) bool {
			return d.Severity == analysis.SeverityError
		}),
		func(d analysis.Diagnostic) string {
			return d.String()
		},
	)

	if len(errMsgs) > 0 {
		return nil, fmt.Errorf("analysis error in %s: %s", srcFile, strings.Join(errMsgs, "; "))
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

// BuildFuzz compiles an on-demand compact single-table fuzz suite for the contract.
func (b *Builder) BuildFuzz(ctx context.Context, srcFile, outFile string) (*Result, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	root, err := b.parser.ParseFile(srcFile)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", srcFile, err)
	}

	if len(root.Structs) == 0 {
		return &Result{
			SourceFile: srcFile,
			Skipped:    true,
		}, nil
	}

	code, err := b.emitter.EmitFuzz(root)
	if err != nil {
		return nil, fmt.Errorf("emit fuzz for %s: %w", srcFile, err)
	}

	targetOut := outFile
	if targetOut == "" {
		dir := filepath.Dir(srcFile)
		base := filepath.Base(srcFile)
		ext := filepath.Ext(base)
		targetOut = filepath.Join(dir, strings.TrimSuffix(base, ext)+"_fuzz_test.go")
	}

	if !b.cfg.DryRun {
		if err := os.WriteFile(targetOut, code, 0o600); err != nil {
			return nil, fmt.Errorf("write fuzz %s: %w", targetOut, err)
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

// BuildMock compiles an in-memory virtual test server (_mock.gen.go) for integration testing.
func (b *Builder) BuildMock(ctx context.Context, srcFile, outFile string) (*Result, error) {
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

	if b.cfg.FixturesFlag {
		rootDir := b.cfg.RootDir
		if rootDir == "" {
			rootDir = "."
		}

		for _, svc := range root.Services {
			_ = PopulateMockFixtures(rootDir, svc)
		}
	}

	code, err := b.emitter.EmitMock(root)
	if err != nil {
		return nil, fmt.Errorf("emit mock for %s: %w", srcFile, err)
	}

	targetOut := outFile
	if targetOut == "" {
		dir := filepath.Dir(srcFile)
		base := filepath.Base(srcFile)
		ext := filepath.Ext(base)
		targetOut = filepath.Join(dir, strings.TrimSuffix(base, ext)+"_mock.gen.go")
	}

	if !b.cfg.DryRun {
		if err := os.WriteFile(targetOut, code, 0o600); err != nil {
			return nil, fmt.Errorf("write mock %s: %w", targetOut, err)
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

// CollectOptions configures recursive file discovery.
type CollectOptions struct {
	MaxDepth int // Max folder recursion depth (default: 6, -1 for unlimited)
	MaxFiles int // Max files to collect (default: 5000)
}

// CollectInputFiles resolves input file paths from flags, environment variables, or path patterns (e.g. ./...).
func CollectInputFiles(fileFlag string, args []string, opts ...CollectOptions) []string {
	var rawTargets []string

	if fileFlag != "" {
		for _, f := range strings.Split(fileFlag, ",") {
			if strings.TrimSpace(f) != "" {
				rawTargets = append(rawTargets, strings.TrimSpace(f))
			}
		}
	} else if gofile := os.Getenv("GOFILE"); gofile != "" && len(args) == 0 {
		rawTargets = []string{gofile}
	} else if len(args) == 0 {
		rawTargets = []string{"./..."}
	} else {
		for _, a := range args {
			for _, item := range strings.Split(a, ",") {
				if strings.TrimSpace(item) != "" {
					rawTargets = append(rawTargets, strings.TrimSpace(item))
				}
			}
		}
	}

	maxDepth := 6
	maxFiles := 5000

	if len(opts) > 0 {
		if opts[0].MaxDepth > 0 {
			maxDepth = opts[0].MaxDepth
		} else if opts[0].MaxDepth == -1 {
			maxDepth = 999999
		}

		if opts[0].MaxFiles > 0 {
			maxFiles = opts[0].MaxFiles
		}
	}

	var matched []string

	seen := make(map[string]bool)

	for _, target := range rawTargets {
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

			// Guard: refuse unbounded scan on raw system root directory or user home directory
			if isUserHomeOrSystemDir(baseDir) {
				continue
			}

			// #nosec G703,G304
			_ = filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d == nil {
					return err
				}

				rel, rErr := filepath.Rel(baseDir, path)
				if rErr == nil && strings.Count(filepath.ToSlash(rel), "/") > maxDepth {
					if d.IsDir() {
						return filepath.SkipDir
					}

					return nil
				}

				if d.IsDir() {
					if isIgnoredDirectory(d.Name()) {
						return filepath.SkipDir
					}

					return nil
				}

				if IsEligibleGoFile(path) && !seen[path] {
					seen[path] = true

					matched = append(matched, path)
					if len(matched) >= maxFiles {
						return filepath.SkipAll
					}
				}

				return nil
			})

			continue
		}

		// #nosec G703,G304
		fi, err := os.Stat(target)
		if err != nil {
			// Try resolving as a symbolic contract name from .vortex.yml (e.g. "AntigravityAPI")
			if resolved := project.ResolveTargetToPath(target); resolved != "" {
				// #nosec G703,G304
				if rfi, rErr := os.Stat(resolved); rErr == nil && !rfi.IsDir() {
					if !seen[resolved] {
						seen[resolved] = true
						matched = append(matched, resolved)
					}

					continue
				}
			}

			continue
		}

		if fi.IsDir() {
			if isUserHomeOrSystemDir(target) {
				continue
			}

			// #nosec G703,G304
			_ = filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d == nil {
					return err
				}

				rel, rErr := filepath.Rel(target, path)
				if rErr == nil && strings.Count(filepath.ToSlash(rel), "/") > maxDepth {
					if d.IsDir() {
						return filepath.SkipDir
					}

					return nil
				}

				if d.IsDir() {
					if isIgnoredDirectory(d.Name()) {
						return filepath.SkipDir
					}

					return nil
				}

				if IsEligibleGoFile(path) && !seen[path] {
					seen[path] = true

					matched = append(matched, path)
					if len(matched) >= maxFiles {
						return filepath.SkipAll
					}
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

// QuickCheckCandidate checks if a Go file contains @aoni or @service directives.
func QuickCheckCandidate(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 65536)

	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false
	}

	content := buf[:n]

	return bytes.Contains(content, []byte("@aoni:service")) ||
		bytes.Contains(content, []byte("@service")) ||
		bytes.Contains(content, []byte("@aoni:endpoint")) ||
		bytes.Contains(content, []byte("@aoni:mirror")) ||
		bytes.Contains(content, []byte("@mirror"))
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

func isSystemRoot(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	clean := filepath.Clean(abs)

	if clean == "/" || clean == "\\" || clean == "." {
		return clean == "/" || clean == "\\"
	}

	vol := filepath.VolumeName(clean)
	if vol != "" && (clean == vol || clean == vol+`\` || clean == vol+`/`) {
		return true
	}

	return false
}

func isIgnoredDirectory(name string) bool {
	if strings.HasPrefix(name, ".") && name != "." && name != ".." {
		return true
	}

	switch strings.ToLower(name) {
	case "vendor", "node_modules", "testdata", "appdata", "windows",
		"program files", "program files (x86)", "$recycle.bin", "system volume information",
		"tmp", "temp", "dist", "build", "out", "bin", "obj", "desktop", "documents",
		"downloads", "pictures", "videos", "music", "onedrive", "virtualbox vms", "scoop":
		return true
	default:
		return false
	}
}

func isUserHomeOrSystemDir(path string) bool {
	if isSystemRoot(path) {
		return true
	}

	clean := filepath.Clean(path)

	home, err := os.UserHomeDir()
	if err == nil && home != "" && clean == filepath.Clean(home) {
		return true
	}

	return false
}
