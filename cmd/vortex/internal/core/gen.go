// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
)

// CmdGen compiles and generates zero-allocation Go client facades from AST contracts.
type CmdGen struct{}

func (c *CmdGen) Name() string      { return "gen" }
func (c *CmdGen) Aliases() []string { return []string{"generate", "watch", "build"} }
func (c *CmdGen) Synopsis() string {
	return "Compile and generate zero-allocation Go clients (default)"
}
func (c *CmdGen) Usage() string { return "vortex gen [flags] [packages/files...]" }

func (c *CmdGen) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fileFlag    string
		outFlag     string
		pkgFlag     string
		watch       bool
		verbose     bool
		harnessFlag bool
		fuzzFlag    bool
		mockFlag    bool
	)

	base.StringVar(
		fs,
		&fileFlag,
		"file",
		"f",
		"",
		"Path to source file containing @aoni contracts (or set via $GOFILE)",
	)
	base.StringVar(
		fs,
		&outFlag,
		"out",
		"o",
		"",
		"Path to output generated .gen.go file (default: <filename>.gen.go)",
	)
	base.StringVar(fs, &pkgFlag, "pkg", "p", "", "Override package name in generated code")
	base.BoolVar(fs, &watch, "watch", "w", false, "Watch source tree and auto-generate on change")
	base.BoolVar(fs, &verbose, "v", "", false, "Enable verbose compilation logging")
	base.BoolVar(
		fs,
		&harnessFlag,
		"harness",
		"",
		false,
		"Generate test, load, and benchmark harness (api_harness.gen.go)",
	)
	base.BoolVar(
		fs,
		&fuzzFlag,
		"fuzz",
		"",
		false,
		"Generate on-demand compact panic-free wire fuzzer (api_fuzz_test.go)",
	)
	base.BoolVar(fs, &mockFlag, "mock", "m", false, "Generate mock test client harness (<filename>_mock.gen.go)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex gen — Compile Zero-Allocation Go Clients from AST Contracts\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex gen [flags] [packages/files...]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  vortex gen ./pkg/api/api.go                     # Compile specific contract file\n")
		fmt.Fprintf(stderr, "  vortex gen MakerSuiteAPI -mock                  # Compile client and mock harness\n")
		fmt.Fprintf(
			stderr,
			"  vortex gen ./...                                # Compile all contracts across workspace\n",
		)
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	rt, _ := base.NewRuntime("")

	var files []string
	if fileFlag != "" {
		if ct, err := rt.ResolveContract(fileFlag); err == nil && ct != nil {
			files = []string{ct.AbsPath}
		} else {
			files = []string{fileFlag}
		}
	} else if len(posArgs) > 0 {
		for _, arg := range posArgs {
			if ct, err := rt.ResolveContract(arg); err == nil && ct != nil {
				files = append(files, ct.AbsPath)
			}
		}
	}

	if len(files) == 0 {
		files = rt.CollectFiles(nil)
	}

	if len(files) == 0 {
		return errors.New("no Go source files found")
	}

	b := builder.New(builder.Config{
		OutFlag:     outFlag,
		PkgFlag:     pkgFlag,
		Verbose:     verbose,
		HarnessFlag: harnessFlag,
	})

	results, err := b.BuildFiles(ctx, files)
	if err != nil {
		return err
	}

	generatedCount := 0
	for _, res := range results {
		if res.Skipped {
			if verbose {
				fmt.Fprintf(stdout, "vortex: skipping %s (no @aoni directives found)\n", res.SourceFile)
			}

			continue
		}

		generatedCount++

		fmt.Fprintf(
			stdout,
			"✔ Generated %s (%d bytes, %d service(s), %d dto(s))\n",
			res.OutputFile,
			res.BytesCount,
			res.ServicesCount,
			res.StructsCount,
		)

		if mockFlag {
			mockRes, mErr := b.BuildMock(ctx, res.SourceFile, "")
			if mErr == nil && mockRes != nil && !mockRes.Skipped {
				fmt.Fprintf(stdout, "✔ Generated mock harness %s (%d bytes)\n", mockRes.OutputFile, mockRes.BytesCount)
			}
		}

		if fuzzFlag {
			fuzzRes, fErr := b.BuildFuzz(ctx, res.SourceFile, "")
			if fErr == nil && fuzzRes != nil && !fuzzRes.Skipped {
				fmt.Fprintf(
					stdout,
					"✔ Generated on-demand fuzzer %s (%d bytes)\n",
					fuzzRes.OutputFile,
					fuzzRes.BytesCount,
				)
			}
		}
	}

	if generatedCount == 0 && !watch {
		fmt.Fprintf(
			stdout,
			"⚡ Scanned %d Go file(s) (0 contracts with @aoni:service found)\n",
			len(files),
		)
	}

	if watch {
		fmt.Fprintf(
			stdout,
			"\n[vortex] Watching for file changes across %d files... (Press Ctrl+C to stop)\n",
			len(files),
		)

		return b.Watch(ctx, files, func(f string, res *builder.Result, bErr error) {
			if bErr != nil {
				fmt.Fprintf(stderr, "[%s] vortex error in %s: %v\n", time.Now().Format("15:04:05"), f, bErr)
				return
			}

			if res != nil && !res.Skipped {
				fmt.Fprintf(
					stdout,
					"[%s] Change detected: regenerated %s (%d bytes)\n",
					time.Now().Format("15:04:05"),
					res.OutputFile,
					res.BytesCount,
				)
			}
		})
	}

	return nil
}
