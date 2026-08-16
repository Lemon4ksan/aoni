// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// vortex is the official zero-allocation AST-driven code generator and OpenAPI 3.1 toolchain for Aoni.
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

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

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
		scopeFlag   = flag.String(
			"scope",
			"",
			"Filter directives by scope (service, socket, method, param, struct, pipeline)",
		)
		jsonFlag = flag.Bool("json", false, "Output list of directives as JSON")
		mdFlag   = flag.Bool("markdown", false, "Output list of directives as Markdown")
	)

	flag.Usage = func() {
		fmt.Fprintf(
			os.Stderr,
			"vortex is a Unified Zero-Allocation AST Toolchain and Engine Suite for projects using aoni\n\n",
		)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(
			os.Stderr,
			"  vortex [flags] [packages/files...]               # Compile and generate zero-allocation Go client (default: ./...)\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex check [flags] [packages/files...]         # Static contract linter & diagnostic inspector (supports --fix)\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex watch [packages/files...]                 # Watch source tree and auto-generate on change\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex bench [flags]                             # Silicon hardware inspection & engine benchmark\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex cover [-file=coverage.out] [-sort=percent]# Deduplicated core test coverage analyzer\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex oapi import -spec=<spec.json> [-split]    # Import OpenAPI/Swagger into Aoni contract\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex oapi export -file=<api.go> [-out=spec]    # Export Aoni contract to OpenAPI 3.1\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex proto -src=<proto_dir> -out=<go_dir>      # Compile Protobuf definitions with vtproto\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex list [flags]                              # List all available @aoni DSL directives\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex pipelines                                 # List all Wire-Transform pipeline stages\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex example [http|ws|socket|pipeline]         # Scaffold ready-made contract templates\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex explain <directive>                       # Show documentation and syntax for directive\n\n",
		)
		fmt.Fprintf(os.Stderr, "Flags (Generation):\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(
			os.Stderr,
			"  vortex                                           # Generate all contracts in current directory tree\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex bench -quick                              # Run fast silicon benchmark on machine\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex cover                                     # Analyze core test coverage\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex watch                                     # Watch and auto-rebuild on file changes\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex check                                     # Validate all contracts in project\n",
		)
		fmt.Fprintf(
			os.Stderr,
			"  vortex oapi import -spec=swagger.json -pkg=bptf -out=pkg/services/bptf/api.go -split\n",
		)
		fmt.Fprintf(os.Stderr, "  vortex proto -src=proto/steam -out=pkg/protobuf/steam\n")
		fmt.Fprintf(os.Stderr, "  //go:generate go run github.com/lemon4ksan/aoni/cmd/vortex -file=$GOFILE\n")
	}

	flag.Parse()

	args := flag.Args()

	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h", "-help":
			flag.Usage()
			return

		case "version", "--version":
			fmt.Println(
				"vortex v0.6.0 — Unified Zero-Allocation AST Toolchain and Engine Suite for projects using aoni",
			)

			return

		case "bench", "benchmark":
			runBench(args[1:])
			return

		case "cover", "coverage":
			runCover(args[1:])
			return

		case "export":
			runExport(args[1:])
			return

		case "import":
			runImport(args[1:])
			return

		case "oapi", "openapi":
			if len(args) > 1 {
				sub := args[1]
				switch sub {
				case "export":
					runExport(args[2:])
					return
				case "import":
					runImport(args[2:])
					return
				}
			}

			runImport(args[1:])

			return

		case "proto", "protobuf", "protoc":
			runProto(args[1:])
			return

		case "list", "directives", "linters":
			listCmd := flag.NewFlagSet("list", flag.ExitOnError)
			lScope := listCmd.String(
				"scope",
				*scopeFlag,
				"Filter directives by scope (service, socket, method, param, struct, pipeline)",
			)
			lJSON := listCmd.Bool("json", *jsonFlag, "Output list of directives as JSON")
			lMD := listCmd.Bool("markdown", *mdFlag, "Output list of directives as Markdown")
			_ = listCmd.Parse(args[1:])

			runList(*lScope, *lJSON, *lMD)

			return

		case "pipelines", "pipeline", "stages":
			runList("pipeline", *jsonFlag, *mdFlag)
			return

		case "example", "examples", "template", "templates", "init":
			exCmd := flag.NewFlagSet("example", flag.ExitOnError)
			exOut := exCmd.String("out", *outFlag, "Write template source code to file")
			_ = exCmd.Parse(args[1:])

			kind := ""
			if len(exCmd.Args()) > 0 {
				kind = exCmd.Args()[0]
			}

			runExample(kind, *exOut)

			return

		case "explain", "doc", "help-directive":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "vortex explain: directive name required (e.g. 'vortex explain form')\n")
				os.Exit(1)
			}

			runExplain(args[1])

			return

		case "watch":
			watchArgs := args[1:]

			files := collectInputFiles(*fileFlag, watchArgs)
			if len(files) == 0 {
				fmt.Fprintf(os.Stderr, "vortex watch: no Go source files found to watch\n")
				os.Exit(1)
			}

			fmt.Printf("\n[vortex] Watching for file changes across %d files... (Press Ctrl+C to stop)\n", len(files))
			watchLoop(files, *outFlag, *pkgFlag, *verboseFlag)

			return

		case "check", "lint", "inspect":
			runCheck(args[1:])
			return

		case "gen", "generate":
			args = args[1:]
		}
	}

	files := collectInputFiles(*fileFlag, args)
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "vortex: no Go source files found in the specified path(s)\n")
		os.Exit(1)
	}

	buildAll(files, *outFlag, *pkgFlag, *verboseFlag)

	if *watchFlag {
		fmt.Printf("\n[vortex] Watching for file changes across %d files... (Press Ctrl+C to stop)\n", len(files))
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
		args = []string{"./..."}
	}

	var matched []string

	seen := make(map[string]bool)

	for _, target := range args {
		target = filepath.Clean(target)

		// Handle recursive wildcard patterns like ./..., ..., ./pkg/..., pkg/...
		if strings.HasSuffix(target, "/...") || target == "./..." || strings.HasSuffix(target, `\...`) ||
			target == "..." ||
			target == "." {
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

				if isEligibleGoFile(path) && !seen[path] {
					seen[path] = true
					matched = append(matched, path)
				}

				return nil
			})

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
	var errCount int

	for _, file := range files {
		out := outFlag
		if out == "" {
			dir := filepath.Dir(file)
			base := filepath.Base(file)
			ext := filepath.Ext(base)
			out = filepath.Join(dir, strings.TrimSuffix(base, ext)+".gen.go")
		}

		if err := compileFile(file, out, pkgFlag, verbose); err != nil {
			errCount++
		}
	}

	if errCount > 0 {
		os.Exit(1)
	}
}

func compileFile(inputFile, outputFile, pkgFlag string, verbose bool) error {
	p := parser.NewParser()

	root, err := p.ParseFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vortex: error parsing %s: %v\n", inputFile, err)
		return err
	}

	if pkgFlag != "" {
		root.PackageName = pkgFlag
	}

	hasTargets := len(root.Services) > 0 || len(root.Tuples) > 0 || len(root.Bitpacks) > 0
	if !hasTargets {
		for _, st := range root.Structs {
			if st.GenValueEncoder {
				hasTargets = true
				break
			}
		}
	}

	if !hasTargets {
		if verbose {
			fmt.Printf("vortex: skipping %s (no @aoni directives found)\n", inputFile)
		}

		return nil
	}

	analyzer := analysis.NewAnalyzer()
	diags := analyzer.Analyze(root)

	hasErrors := false
	for _, d := range diags {
		if d.Severity == analysis.SeverityError {
			hasErrors = true

			fmt.Fprintf(os.Stderr, "vortex: [ERROR] %s: %s\n", d.Target, d.Message)
		} else if verbose {
			fmt.Printf("vortex: [WARN] %s: %s\n", d.Target, d.Message)
		}
	}

	if hasErrors {
		fmt.Fprintf(os.Stderr, "vortex: compilation aborted for %s\n", inputFile)
		return fmt.Errorf("compilation error in %s", inputFile)
	}

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	em := emitter.NewEmitter()

	code, err := em.Emit(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vortex: emission error for %s: %v\n", inputFile, err)
		return err
	}

	if err := os.WriteFile(outputFile, code, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "vortex: failed writing %s: %v\n", outputFile, err)
		return err
	}

	fmt.Printf("✔ Generated %s (%d bytes, %d service(s), %d dto(s))\n",
		outputFile, len(code), len(root.Services), len(root.Structs))

	return nil
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

				if err := compileFile(f, out, pkgFlag, verbose); err != nil {
					fmt.Fprintf(os.Stderr, "vortex: compilation error: %v\n", err)
				}
			}
		}
	}
}
