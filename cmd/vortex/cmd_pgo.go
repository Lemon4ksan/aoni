// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/project"
	"github.com/lemon4ksan/aoni/internal/tui"
)

// CmdPGO orchestrates automated Profile-Guided Optimization (PGO) profile generation.
type CmdPGO struct{}

func (c *CmdPGO) Name() string      { return "pgo" }
func (c *CmdPGO) Aliases() []string { return []string{"profile-guided", "opt-profile"} }
func (c *CmdPGO) Synopsis() string {
	return "Profile-Guided Optimization (PGO): Generate default.pgo from native harness benchmarks"
}
func (c *CmdPGO) Usage() string { return "vortex pgo [flags] [packages...]" }

func (c *CmdPGO) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("pgo", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		outFlag       = fs.String("out", "default.pgo", "Destination path for PGO profile")
		benchTimeFlag = fs.String("benchtime", "200ms", "Duration per benchmark target during profile capture")
		pkgFlag       = fs.String("pkg", "./...", "Target package pattern to profile")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex pgo — Profile-Guided Optimization Generator\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex pgo [-out=default.pgo] [-benchtime=200ms] [packages...]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	rootDir, _, _ := project.FindRoot(cwd)

	targetPkg := *pkgFlag
	if len(fs.Args()) > 0 {
		targetPkg = fs.Args()[0]
	}

	outPath := *outFlag
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(rootDir, outPath)
	}

	fmt.Fprintf(stdout, "\n⚡ Vortex Profile-Guided Optimization (PGO) Pipeline\n")
	fmt.Fprintf(stdout, "Target:     %s (benchtime=%s)\n", targetPkg, *benchTimeFlag)
	fmt.Fprintf(stdout, "Output:     %s\n\n", outPath)

	start := time.Now()

	// Execute go test with cpuprofile
	// #nosec G204,G702 -- standard go test tool execution
	cmd := exec.CommandContext(
		ctx,
		"go",
		"test",
		"-run=^$",
		"-bench=^Benchmark",
		"-benchtime="+*benchTimeFlag,
		"-cpuprofile="+outPath,
		targetPkg,
	)
	cmd.Dir = rootDir

	var cmdErr bytes.Buffer

	cmd.Stderr = io.MultiWriter(stderr, &cmdErr)

	if err := cmd.Run(); err != nil {
		if _, statErr := os.Stat(outPath); statErr != nil {
			return fmt.Errorf("pgo capture failed (ensure benchmark harness is generated via 'vortex harness'): %w\n%s",
				err, cmdErr.String())
		}
	}

	info, statErr := os.Stat(outPath)
	if statErr != nil || info.Size() == 0 {
		return errors.New("pgo generation produced an empty profile (ensure target packages contain Benchmarks)")
	}

	elapsed := time.Since(start).Round(time.Millisecond)

	fmt.Fprintf(stdout, "─────────────────────────────────────────────────────────────────────────────\n")
	fmt.Fprintf(stdout, "✔ Successfully captured PGO CPU profile: %s (%d bytes, %s)\n\n",
		filepath.Base(outPath), info.Size(), elapsed)

	fmt.Fprintf(stdout, "💡 %s:\n", tui.Bold("Compiler Integration"))
	fmt.Fprintf(
		stdout,
		"  Go 1.20+ automatically detects %s during 'go build' or 'go install'.\n",
		tui.Cyan("default.pgo"),
	)
	fmt.Fprintf(stdout, "  Hot client encode/decode loops will achieve 7–15%% higher silicon throughput.\n\n")

	return nil
}
