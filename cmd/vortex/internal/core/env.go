// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/internal/codegen/pipeline"
)

// CmdEnv scans Go contracts for referenced environment variables and generates .env templates.
type CmdEnv struct{}

func (c *CmdEnv) Name() string      { return "env" }
func (c *CmdEnv) Aliases() []string { return []string{"dotenv"} }
func (c *CmdEnv) Synopsis() string {
	return "Scan contracts for ${VAR_NAME} references and generate .env.example / .env"
}

func (c *CmdEnv) Usage() string {
	return "vortex env [file.go|contract] [--out=.env.example] [--fill] [flags]"
}

func (c *CmdEnv) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		outFile string
		fill    bool
		dir     string
	)

	base.StringVar(
		fs,
		&outFile,
		"out",
		"o",
		".env.example",
		"Output file for environment variables (use '-' for stdout)",
	)
	base.BoolVar(fs, &fill, "fill", "f", false, "Pre-fill variables from local .vortex/cache/secrets.json vault")
	base.StringVar(fs, &dir, "dir", "d", "", "Target workspace directory (default: current root)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex env — Contract Environment & Secrets Template Generator\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex env [contract.go] [--out=.env.example] [--fill]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex env                              # Scan all contracts and write .env.example\n")
		fmt.Fprintf(
			stderr,
			"  vortex env --fill --out=.env.local      # Generate local .env filled from secrets vault\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex env pkg/api/api.go --out=-       # Print required variables for contract to stdout\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	rt, err := base.NewRuntime(dir)
	if err != nil {
		return err
	}

	var targetFiles []string
	if len(posArgs) > 0 {
		for _, arg := range posArgs {
			if target, err := rt.ResolveContract(arg); err == nil && target != nil {
				targetFiles = append(targetFiles, target.AbsPath)
			}
		}
	}

	if len(targetFiles) == 0 {
		targetFiles = rt.CollectFiles(nil)
	}

	tp, err := pipeline.NewTrafficPipeline(rt.RootDir)
	if err != nil {
		return err
	}

	count, err := tp.FillEnv(targetFiles, outFile, fill)
	if err != nil {
		return err
	}

	if outFile != "-" {
		fmt.Fprintf(stdout, "Generated %s with %d required variable(s)\n", outFile, count)
	}

	return nil
}
