// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package perf

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
)

// Cmd translates low-level benchmarks and pprof traces into an executive-ready performance dashboard.
type Cmd struct{}

// NewCommand returns a new perf command instance.
func NewCommand() base.Command {
	return &Cmd{}
}

func (c *Cmd) Name() string      { return "perf" }
func (c *Cmd) Aliases() []string { return []string{"prof", "profile"} }
func (c *Cmd) Synopsis() string {
	return "Inspect endpoint throughput, allocations, harness benchmarks, and pprof profiles"
}

func (c *Cmd) Usage() string {
	return "vortex perf [prof|bench|harness|cover|pgo] [flags]"
}

func (c *Cmd) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "bench", "benchmark":
			return (&CmdBench{}).Run(ctx, args[1:], stdout, stderr)
		case "cover", "coverage":
			return (&CmdCover{}).Run(ctx, args[1:], stdout, stderr)
		case "harness", "test-harness":
			return (&CmdHarness{}).Run(ctx, args[1:], stdout, stderr)
		case "pgo":
			return (&CmdPGO{}).Run(ctx, args[1:], stdout, stderr)
		case "prof", "report":
			args = args[1:]
		case "-h", "--help", "help":
			c.printUsage(stdout)
			return nil
		}
	}

	return c.runProf(ctx, args, stdout, stderr)
}

func (c *Cmd) printUsage(w io.Writer) {
	fmt.Fprintf(w, "vortex perf — Performance Dashboard & Profiler Toolchain Hub\n\n")
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  vortex perf [prof|bench|cover|harness|pgo] [flags] [packages...]\n\n")
	fmt.Fprintf(w, "Subcommands:\n")
	fmt.Fprintf(w, "  prof       Inspect endpoint throughput, allocations, and pprof metrics (default)\n")
	fmt.Fprintf(w, "  bench      Silicon hardware inspection & network engine micro-benchmarks\n")
	fmt.Fprintf(w, "  cover      Deduplicated test coverage analyzer and HTML visualizer\n")
	fmt.Fprintf(w, "  harness    Generate zero-allocation integration test and benchmark harness\n")
	fmt.Fprintf(w, "  pgo        Generate and manage Profile-Guided Optimization profiles\n\n")
	fmt.Fprintf(w, "Examples:\n")
	fmt.Fprintf(w, "  vortex perf\n")
	fmt.Fprintf(w, "  vortex perf bench\n")
	fmt.Fprintf(w, "  vortex perf cover\n")
	fmt.Fprintf(w, "  vortex perf harness\n")
	fmt.Fprintf(w, "  vortex perf pgo\n")
}
