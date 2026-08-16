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
	"os"

	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdStatus implements the `vortex status` 360-degree API guardian dashboard command.
type CmdStatus struct{}

func (c *CmdStatus) Name() string      { return "status" }
func (c *CmdStatus) Aliases() []string { return []string{"st", "health"} }
func (c *CmdStatus) Synopsis() string {
	return "Show 360° synchronization health of workspace contracts, generated code, and upstream drifts"
}
func (c *CmdStatus) Usage() string { return "vortex status [-all] [-check] [-json] [targets...]" }

func (c *CmdStatus) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		allFlag   = fs.Bool("all", false, "Inspect all workspace contracts regardless of current subfolder")
		allShort  = fs.Bool("A", false, "Alias for --all")
		checkFlag = fs.Bool(
			"check",
			false,
			"CI mode: exit code 1 if any generated code is stale or breaking drifts exist",
		)
		jsonFlag = fs.Bool("json", false, "Output status report in JSON format")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex status — API Guardian Workspace Health Dashboard\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex status [-all] [-check] [-json] [targets...]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	cfg, err := project.Load(cwd)
	if err != nil {
		return fmt.Errorf("loading project configuration: %w", err)
	}

	targetNames := fs.Args()

	var contractsToInspect []project.ContractConfig

	if *allFlag || *allShort || len(targetNames) > 0 {
		contractsToInspect = cfg.FilterContext("", targetNames...)
	} else {
		contractsToInspect = cfg.FilterContext(cwd)
	}

	engine := project.NewStatusEngine()
	report := engine.Inspect(cfg, contractsToInspect)

	if *jsonFlag {
		data, err := report.RenderJSON()
		if err != nil {
			return fmt.Errorf("rendering JSON status: %w", err)
		}

		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprint(stdout, report.Render(true))
	}

	if *checkFlag && report.HasIssues() {
		return errors.New("vortex status --check: workspace has stale generated files or breaking drifts")
	}

	return nil
}
