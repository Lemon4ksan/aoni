// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
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
		fileFlag    = fs.String("file", "", "Path to source file containing @aoni contracts (or set via $GOFILE)")
		outFlag     = fs.String("out", "", "Path to output generated .gen.go file (default: <filename>.gen.go)")
		pkgFlag     = fs.String("pkg", "", "Override package name in generated code")
		watch       = fs.Bool("watch", false, "Watch source tree and auto-generate on change")
		verbose     = fs.Bool("v", false, "Enable verbose compilation logging")
		harnessFlag = fs.Bool("harness", false, "Generate test, load, and benchmark harness (api_harness.gen.go)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex gen — Zero-Allocation AST Code Generator\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(
			stderr,
			"  vortex [gen] [-file=api.go] [-out=api.gen.go] [-pkg=pkgname] [-harness] [-watch] [-v] [paths...]\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	files := builder.CollectInputFiles(*fileFlag, fs.Args())
	if len(files) == 0 {
		return errors.New("no Go source files found in the specified path(s)")
	}

	b := builder.New(builder.Config{
		OutFlag:     *outFlag,
		PkgFlag:     *pkgFlag,
		Verbose:     *verbose,
		HarnessFlag: *harnessFlag,
	})

	results, err := b.BuildFiles(ctx, files)
	if err != nil {
		return err
	}

	for _, res := range results {
		if res.Skipped {
			if *verbose {
				fmt.Fprintf(stdout, "vortex: skipping %s (no @aoni directives found)\n", res.SourceFile)
			}

			continue
		}

		fmt.Fprintf(
			stdout,
			"✔ Generated %s (%d bytes, %d service(s), %d dto(s))\n",
			res.OutputFile,
			res.BytesCount,
			res.ServicesCount,
			res.StructsCount,
		)
	}

	if *watch {
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
