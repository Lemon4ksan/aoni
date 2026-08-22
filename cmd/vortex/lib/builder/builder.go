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
	"iter"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/analysis"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/optimizer"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
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
}

// New creates a new [Builder] configured with the provided [Config].
func New(cfg Config) *Builder {
	return &Builder{
		cfg:       cfg,
		parser:    parser.NewParser(),
		analyzer:  analysis.NewAnalyzer(),
		optimizer: optimizer.NewOptimizer(),
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

	if !hasCompilationTargets(root) {
		return &Result{SourceFile: srcFile, Skipped: true}, nil
	}

	if err := b.validateAnalysis(root, srcFile); err != nil {
		return nil, err
	}

	b.optimizer.Optimize(root)

	code, err := emitter.Emit(root)
	if err != nil {
		return nil, fmt.Errorf("emit %s: %w", srcFile, err)
	}

	targetOut := resolveOutputPath(srcFile, outFile, ".gen.go")
	if err := writeGeneratedFile(targetOut, code, b.cfg.DryRun); err != nil {
		return nil, err
	}

	if b.cfg.HarnessFlag && len(root.Services) > 0 {
		harnessTarget := resolveOutputPath(srcFile, "", "_harness.gen.go")
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

func hasCompilationTargets(root *ir.RootIR) bool {
	if root == nil {
		return false
	}
	return len(root.Services) > 0 || len(root.Tuples) > 0 ||
		len(root.Bitpacks) > 0 || len(root.Unions) > 0 ||
		generic.Any(root.Structs, func(st *ir.StructIR) bool { return st.GenValueEncoder })
}

func (b *Builder) validateAnalysis(root *ir.RootIR, srcFile string) error {
	diags := b.analyzer.Analyze(root)
	errDiags := generic.Filter(diags, func(d analysis.Diagnostic) bool {
		return d.Severity == analysis.SeverityError
	})

	if len(errDiags) > 0 {
		errMsgs := generic.Map(errDiags, func(d analysis.Diagnostic) string {
			return d.String()
		})
		return fmt.Errorf("analysis error in %s: %s", srcFile, strings.Join(errMsgs, "; "))
	}

	return nil
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
		return &Result{SourceFile: srcFile, Skipped: true}, nil
	}

	code, err := emitter.EmitHarness(root)
	if err != nil {
		return nil, fmt.Errorf("emit harness for %s: %w", srcFile, err)
	}

	targetOut := resolveOutputPath(srcFile, outFile, "_harness.gen.go")
	if err := writeGeneratedFile(targetOut, code, b.cfg.DryRun); err != nil {
		return nil, err
	}

	if !b.cfg.DryRun {
		b.emitOptionalHarnessTests(root, targetOut)
	}

	return &Result{
		SourceFile:    srcFile,
		OutputFile:    targetOut,
		ServicesCount: len(root.Services),
		BytesCount:    len(code),
		Code:          code,
	}, nil
}

func (b *Builder) emitOptionalHarnessTests(root *ir.RootIR, targetOut string) {
	testCode, err := emitter.EmitHarnessTests(root)
	if err != nil || len(testCode) == 0 {
		return
	}

	testOut := strings.TrimSuffix(targetOut, ".gen.go") + "_test.go"
	if strings.HasSuffix(targetOut, ".go") && !strings.HasSuffix(targetOut, ".gen.go") {
		testOut = strings.TrimSuffix(targetOut, ".go") + "_test.go"
	}
	_ = os.WriteFile(testOut, testCode, 0o600)
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
		return &Result{SourceFile: srcFile, Skipped: true}, nil
	}

	code, err := emitter.EmitFuzz(root)
	if err != nil {
		return nil, fmt.Errorf("emit fuzz for %s: %w", srcFile, err)
	}

	targetOut := resolveOutputPath(srcFile, outFile, "_fuzz_test.go")
	if err := writeGeneratedFile(targetOut, code, b.cfg.DryRun); err != nil {
		return nil, err
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
		return &Result{SourceFile: srcFile, Skipped: true}, nil
	}

	if b.cfg.FixturesFlag {
		rootDir := generic.Coalesce(b.cfg.RootDir, ".")
		for _, svc := range root.Services {
			_ = PopulateMockFixtures(rootDir, svc)
		}
	}

	code, err := emitter.EmitMock(root)
	if err != nil {
		return nil, fmt.Errorf("emit mock for %s: %w", srcFile, err)
	}

	targetOut := resolveOutputPath(srcFile, outFile, "_mock.gen.go")
	if err := writeGeneratedFile(targetOut, code, b.cfg.DryRun); err != nil {
		return nil, err
	}

	return &Result{
		SourceFile:    srcFile,
		OutputFile:    targetOut,
		ServicesCount: len(root.Services),
		BytesCount:    len(code),
		Code:          code,
	}, nil
}

func resolveOutputPath(srcFile, customOut, suffix string) string {
	if customOut != "" {
		return customOut
	}
	dir := filepath.Dir(srcFile)
	base := filepath.Base(srcFile)
	ext := filepath.Ext(base)
	return filepath.Join(dir, strings.TrimSuffix(base, ext)+suffix)
}

func writeGeneratedFile(targetPath string, code []byte, dryRun bool) error {
	if dryRun {
		return nil
	}
	if err := os.WriteFile(targetPath, code, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", targetPath, err)
	}
	return nil
}

// BuildFilesSeq compiles files sequentially and yields compilation results lazily as a streaming iterator.
func (b *Builder) BuildFilesSeq(ctx context.Context, files []string) iter.Seq2[*Result, error] {
	return func(yield func(*Result, error) bool) {
		for _, file := range files {
			select {
			case <-ctx.Done():
				yield(nil, ctx.Err())
				return
			default:
			}

			out := b.cfg.OutFlag
			if out == "" || len(files) > 1 {
				out = resolveOutputPath(file, "", ".gen.go")
			}

			res, err := b.BuildFile(ctx, file, out)
			if !yield(res, err) {
				return
			}
		}
	}
}

// BuildFiles compiles a slice of Go source files in sequence.
func (b *Builder) BuildFiles(ctx context.Context, files []string) ([]*Result, error) {
	results := make([]*Result, 0, len(files))

	var (
		lastErr  error
		errCount int
	)

	for res, err := range b.BuildFilesSeq(ctx, files) {
		if err != nil {
			errCount++
			lastErr = err
			continue
		}
		if res != nil {
			results = append(results, res)
		}
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
			b.checkModifiedFiles(ctx, files, modTimes, onChange)
		}
	}
}

func (b *Builder) checkModifiedFiles(
	ctx context.Context,
	files []string,
	modTimes map[string]time.Time,
	onChange func(file string, res *Result, err error),
) {
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}

		if fi.ModTime().After(modTimes[f]) {
			modTimes[f] = fi.ModTime()

			out := b.cfg.OutFlag
			if out == "" || len(files) > 1 {
				out = resolveOutputPath(f, "", ".gen.go")
			}

			res, bErr := b.BuildFile(ctx, f, out)
			if onChange != nil {
				onChange(f, res, bErr)
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
	rawTargets := extractRawTargets(fileFlag, args)
	maxDepth, maxFiles := parseCollectLimits(opts)

	var matched []string
	seen := generic.NewSet[string]()

	for _, target := range rawTargets {
		cleanTarget := filepath.Clean(target)

		if isRecursivePattern(cleanTarget) {
			baseDir := cleanRecursiveBase(cleanTarget)
			walkEligibleGoFiles(baseDir, maxDepth, maxFiles, seen, &matched)
			continue
		}

		fi, err := os.Stat(cleanTarget)
		if err != nil {
			tryResolveSymbolicTarget(cleanTarget, seen, &matched)
			continue
		}

		if fi.IsDir() {
			walkEligibleGoFiles(cleanTarget, maxDepth, maxFiles, seen, &matched)
			continue
		}

		if IsEligibleGoFile(cleanTarget) && !seen.Has(cleanTarget) {
			seen.Add(cleanTarget)
			matched = append(matched, cleanTarget)
		}
	}

	return matched
}

func extractRawTargets(fileFlag string, args []string) []string {
	if fileFlag != "" {
		parts := strings.Split(fileFlag, ",")
		return generic.Filter(generic.Map(parts, strings.TrimSpace), func(s string) bool { return s != "" })
	}

	if gofile := os.Getenv("GOFILE"); gofile != "" && len(args) == 0 {
		return []string{gofile}
	}

	if len(args) == 0 {
		return []string{"./..."}
	}

	var rawTargets []string
	for _, a := range args {
		parts := strings.Split(a, ",")
		for _, item := range parts {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				rawTargets = append(rawTargets, trimmed)
			}
		}
	}
	return rawTargets
}

func parseCollectLimits(opts []CollectOptions) (maxDepth, maxFiles int) {
	maxDepth = 6
	maxFiles = 5000

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

	return maxDepth, maxFiles
}

func isRecursivePattern(target string) bool {
	return strings.HasSuffix(target, "/...") || target == "./..." ||
		strings.HasSuffix(target, `\...`) || target == "..." || target == "."
}

func cleanRecursiveBase(target string) string {
	baseDir := target
	baseDir = strings.TrimSuffix(baseDir, "/...")
	baseDir = strings.TrimSuffix(baseDir, `\...`)
	baseDir = strings.TrimSuffix(baseDir, "...")
	if baseDir == "" || baseDir == "." {
		return "."
	}
	return baseDir
}

func tryResolveSymbolicTarget(target string, seen generic.Set[string], matched *[]string) {
	resolved := project.ResolveTargetToPath(target)
	if resolved == "" {
		return
	}

	rfi, err := os.Stat(resolved)
	if err == nil && !rfi.IsDir() && !seen.Has(resolved) {
		seen.Add(resolved)
		*matched = append(*matched, resolved)
	}
}

func walkEligibleGoFiles(baseDir string, maxDepth, maxFiles int, seen generic.Set[string], matched *[]string) {
	if isUserHomeOrSystemDir(baseDir) {
		return
	}

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

		if IsEligibleGoFile(path) && !seen.Has(path) {
			seen.Add(path)
			*matched = append(*matched, path)
			if len(*matched) >= maxFiles {
				return filepath.SkipAll
			}
		}

		return nil
	})
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
	return !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, ".gen.go")
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
