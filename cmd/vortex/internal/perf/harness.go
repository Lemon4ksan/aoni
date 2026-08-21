// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package perf

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
)

// CmdHarness compiles and emits Unified Test & Bench Harnesses (api_harness.gen.go).
type CmdHarness struct{}

func (c *CmdHarness) Name() string      { return "harness" }
func (c *CmdHarness) Aliases() []string { return []string{"bench-harness", "load-harness"} }
func (c *CmdHarness) Synopsis() string {
	return "Generate zero-allocation test, load, and benchmark harness for Porthack and native Go benchmarks"
}
func (c *CmdHarness) Usage() string { return "vortex perf harness [flags] [packages/files...]" }

func (c *CmdHarness) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("harness", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fileFlag = fs.String("file", "", "Path to source file containing @aoni contracts")
		outFlag  = fs.String("out", "", "Path to output generated harness file (default: <filename>_harness.gen.go)")
		fuzzFlag = fs.Bool("fuzz", false, "Also generate on-demand compact panic-free wire fuzzer (api_fuzz_test.go)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex perf harness — Unified Test & Bench Harness Generator\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex perf harness [-file=api.go] [-out=api_harness.gen.go] [-fuzz] [paths...]\n\n")
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
		OutFlag: *outFlag,
	})

	harnessCount := 0

	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if strings.HasSuffix(file, ".gen.go") || strings.HasSuffix(file, "_test.go") {
			continue
		}

		targetOut := *outFlag
		if targetOut == "" {
			dir := filepath.Dir(file)
			base := filepath.Base(file)
			ext := filepath.Ext(base)
			targetOut = filepath.Join(dir, strings.TrimSuffix(base, ext)+"_harness.gen.go")
		}

		res, err := b.BuildHarness(ctx, file, targetOut)
		if err != nil {
			return fmt.Errorf("failed generating harness for %s: %w", file, err)
		}

		if res.Skipped {
			continue
		}

		harnessCount++

		fmt.Fprintf(
			stdout,
			"✔ Generated Harness: %s (%d bytes, %d service(s))\n",
			res.OutputFile,
			res.BytesCount,
			res.ServicesCount,
		)

		if *fuzzFlag {
			fuzzRes, fErr := b.BuildFuzz(ctx, file, "")
			if fErr == nil && fuzzRes != nil && !fuzzRes.Skipped {
				fmt.Fprintf(
					stdout,
					"✔ Generated Fuzzer: %s (%d bytes)\n",
					fuzzRes.OutputFile,
					fuzzRes.BytesCount,
				)
			}
		}
	}

	if harnessCount == 0 {
		return errors.New("no @aoni:service contracts found to generate harness")
	}

	return nil
}
