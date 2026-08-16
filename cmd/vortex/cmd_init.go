// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdInit initializes and scaffolds a .vortex.yml configuration file from discovered contracts.
type CmdInit struct{}

func (c *CmdInit) Name() string      { return "init" }
func (c *CmdInit) Aliases() []string { return []string{"scaffold"} }
func (c *CmdInit) Synopsis() string {
	return "Scaffold .vortex.yml workspace configuration from auto-discovered contracts"
}
func (c *CmdInit) Usage() string { return "vortex init [-force] [-dir=.]" }

func (c *CmdInit) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		forceFlag = fs.Bool("force", false, "Overwrite existing .vortex.yml configuration file")
		forceF    = fs.Bool("f", false, "Alias for --force")
		dirFlag   = fs.String("dir", "", "Target workspace directory (default: current repository root)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex init — Initialize .vortex.yml Workspace Configuration\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex init [-force] [-dir=.]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	targetDir := *dirFlag
	if targetDir == "" && len(fs.Args()) > 0 {
		targetDir = fs.Arg(0)
	}

	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	force := *forceFlag || *forceF

	cfg, err := project.Init(targetDir, force)
	if err != nil {
		return fmt.Errorf("initializing workspace: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Created %s (%d service(s) configured)\n", cfg.ConfigPath, len(cfg.Contracts))

	for _, ct := range cfg.Contracts {
		fmt.Fprintf(stdout, "  ↳ %-12s -> %s\n", ct.Name, ct.File)
	}

	fmt.Fprintf(stdout, "\nReady: Run `vortex status` to inspect workspace health.\n")

	return nil
}
