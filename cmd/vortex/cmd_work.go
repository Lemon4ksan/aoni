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
	"path/filepath"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdWork orchestrates multi-repo workspace operations (.vortex.work).
type CmdWork struct {
	app *App
}

func (c *CmdWork) Name() string      { return "work" }
func (c *CmdWork) Aliases() []string { return []string{"workspace", "monorepo"} }
func (c *CmdWork) Synopsis() string {
	return "Orchestrate multi-repo workspaces, synchronize contracts, and execute cross-project pipelines"
}

func (c *CmdWork) Usage() string {
	return "vortex work [init|gen|check|prof|status|run <cmd>] [flags]"
}

func (c *CmdWork) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return c.runStatus(ctx, stdout, stderr)
	}

	sub := args[0]
	subArgs := args[1:]

	switch strings.ToLower(sub) {
	case "init":
		return c.runInit(ctx, subArgs, stdout, stderr)
	case "gen", "build", "generate":
		return c.runForward(ctx, "gen", subArgs, stdout, stderr)
	case "check", "lint", "audit":
		return c.runForward(ctx, "check", subArgs, stdout, stderr)
	case "prof", "bench":
		return c.runForward(ctx, "prof", subArgs, stdout, stderr)
	case "status", "list":
		return c.runStatus(ctx, stdout, stderr)
	case "run":
		if len(subArgs) == 0 {
			return errors.New("usage: vortex work run <subcommand> [flags]")
		}

		return c.runForward(ctx, subArgs[0], subArgs[1:], stdout, stderr)

	default:
		return fmt.Errorf("unknown work subcommand %q. Available: init, gen, check, prof, status, run", sub)
	}
}

func (c *CmdWork) runInit(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("work init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fileFlag = fs.String("file", ".vortex.work", "Target workspace file path")
		autoFlag = fs.Bool("auto", true, "Auto-discover child folders with .vortex.yml")
	)

	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	var workspaces []string
	if len(fs.Args()) > 0 {
		workspaces = fs.Args()
	} else if *autoFlag {
		discovered, dErr := project.AutoDiscoverWorkspaces(cwd)
		if dErr == nil && len(discovered) > 0 {
			workspaces = discovered
		}
	}

	if len(workspaces) == 0 {
		workspaces = []string{"./aoni", "./g-man", "./g-man-tf2-public"}
	}

	wc := &project.WorkConfig{
		Version:    1,
		Workspaces: workspaces,
	}

	targetPath := filepath.Join(cwd, *fileFlag)
	if err := project.SaveWork(targetPath, wc); err != nil {
		return fmt.Errorf("failed saving %s: %w", targetPath, err)
	}

	fmt.Fprintf(stdout, "✔ Created multi-repo workspace configuration: %s\n", targetPath)
	fmt.Fprintf(stdout, "  Linked %d workspaces:\n", len(workspaces))

	for _, ws := range workspaces {
		fmt.Fprintf(stdout, "    • %s\n", ws)
	}

	fmt.Fprintf(stdout, "\nRun 'vortex' or 'vortex work gen' to build all workspaces simultaneously.\n")

	return nil
}

func (c *CmdWork) runStatus(_ context.Context, stdout, _ io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	wc, err := project.LoadWork(cwd)
	if err != nil {
		return fmt.Errorf("no .vortex.work file found (run 'vortex work init' to create one): %w", err)
	}

	fmt.Fprintf(stdout, "⚡ Vortex Workspace Orchestrator\n")
	fmt.Fprintf(stdout, "Work File: %s (%d workspaces linked)\n\n", wc.WorkPath, len(wc.Workspaces))

	fmt.Fprintf(stdout, "  %-24s %-12s %-16s %-20s\n", "WORKSPACE", "CONTRACTS", "SERVICES", "STATUS")
	fmt.Fprintf(stdout, "  %s\n", strings.Repeat("─", 80))

	for _, ws := range wc.Workspaces {
		wsPath := filepath.Join(wc.WorkDir, filepath.FromSlash(ws))

		cfg, lErr := project.Load(wsPath)
		if lErr != nil || cfg == nil {
			fmt.Fprintf(stdout, "  %-24s %-12s %-16s %-20s\n", ws, "-", "-", "⚠️ Missing config")
			continue
		}

		contractsCount := len(cfg.Contracts)

		servicesCount := 0
		for _, ct := range cfg.Contracts {
			if ct.Name != "" {
				servicesCount++
			}
		}

		fmt.Fprintf(stdout, "  %-24s %-12d %-16d %-20s\n", ws, contractsCount, servicesCount, "✔ Synchronized")
	}

	fmt.Fprintln(stdout)

	return nil
}

func (c *CmdWork) runForward(ctx context.Context, targetCmd string, args []string, stdout, stderr io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	wc, err := project.LoadWork(cwd)
	if err != nil {
		return fmt.Errorf("no .vortex.work file found: %w", err)
	}

	fmt.Fprintf(
		stdout,
		"⚡ Vortex Multi-Repo Execution: running %q across %d workspaces\n",
		targetCmd,
		len(wc.Workspaces),
	)
	fmt.Fprintf(stdout, "Work File: %s\n\n", wc.WorkPath)

	start := time.Now()
	successCount := 0

	for i, ws := range wc.Workspaces {
		wsPath := filepath.Join(wc.WorkDir, filepath.FromSlash(ws))
		if _, statErr := os.Stat(wsPath); statErr != nil {
			fmt.Fprintf(stderr, "[%d/%d] %s ........... ⚠️ Directory not found\n", i+1, len(wc.Workspaces), ws)
			continue
		}

		fmt.Fprintf(stdout, "─── [%d/%d] Workspace: %s ───\n", i+1, len(wc.Workspaces), ws)

		// Dispatch command in the workspace directory
		cmdInstance := c.lookupCommand(targetCmd)
		if cmdInstance == nil {
			return fmt.Errorf("unknown command %q", targetCmd)
		}

		// Temporarily change working dir or execute
		origWd, _ := os.Getwd()
		_ = os.Chdir(wsPath)

		runErr := cmdInstance.Run(ctx, args, stdout, stderr)
		_ = os.Chdir(origWd)

		if runErr != nil {
			fmt.Fprintf(stderr, "❌ [%s] failed: %v\n\n", ws, runErr)
		} else {
			successCount++

			fmt.Fprintf(stdout, "✔ [%s] completed cleanly\n\n", ws)
		}
	}

	elapsed := time.Since(start).Round(time.Millisecond)

	fmt.Fprintf(stdout, "─────────────────────────────────────────────────────────────────────────────\n")
	fmt.Fprintf(
		stdout,
		"✨ Multi-Repo Pipeline: %d/%d workspaces completed successfully in %s!\n\n",
		successCount,
		len(wc.Workspaces),
		elapsed,
	)

	return nil
}

func (c *CmdWork) lookupCommand(name string) Command {
	if c.app == nil {
		return nil
	}

	return c.app.cmdMap[name]
}
