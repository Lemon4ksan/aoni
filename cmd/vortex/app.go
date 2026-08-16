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
	"runtime"
	"strings"
)

// App manages the lifecycle, subcommand routing, and usage formatting of the Vortex CLI.
type App struct {
	Name        string
	Version     string
	Description string
	Commands    []Command
	cmdMap      map[string]Command
	defaultCmd  Command
	Stdout      io.Writer
	Stderr      io.Writer
}

type commandGroup struct {
	Title    string
	Commands []string
}

var commandGroups = []commandGroup{
	{
		Title: "🚀 Core Workflow & Generation",
		Commands: []string{
			"autopilot", "gen", "mock", "init", "status", "config", "work", "clean",
		},
	},
	{
		Title: "🔍 Quality, Testing & Drift",
		Commands: []string{
			"check", "diff", "review", "accept", "cherrypick", "harness", "bench", "cover",
		},
	},
	{
		Title: "🌐 Traffic & Protocols",
		Commands: []string{
			"record", "import", "export", "oapi", "proto",
		},
	},
	{
		Title: "🛠️ Refactoring & Tooling",
		Commands: []string{
			"refactor", "history", "undo", "source", "log", "tag",
			"blame", "prof", "pgo", "explain", "example", "list", "completion",
		},
	},
}

// NewApp constructs a new CLI application instance.
func NewApp(name, version, description string, commands ...Command) *App {
	app := &App{
		Name:        name,
		Version:     version,
		Description: description,
		Commands:    commands,
		cmdMap:      make(map[string]Command),
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}

	for _, c := range commands {
		app.cmdMap[c.Name()] = c
		for _, alias := range c.Aliases() {
			app.cmdMap[alias] = c
		}

		if c.Name() == "autopilot" {
			app.defaultCmd = c
			if ap, ok := c.(*CmdAutoPilot); ok {
				ap.app = app
			}
		}

		if cw, ok := c.(*CmdWork); ok {
			cw.app = app
		}
	}

	return app
}

// Run parses command arguments and dispatches execution to the appropriate subcommand.
func (a *App) Run(ctx context.Context, args []string) error {
	stdout := a.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := a.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if len(args) == 0 {
		if a.defaultCmd != nil {
			return a.defaultCmd.Run(ctx, nil, stdout, stderr)
		}

		a.PrintUsage(stderr)

		return nil
	}

	first := args[0]

	switch first {
	case "help", "--help", "-h", "-help":
		if len(args) > 1 {
			subName := args[1]
			if cmd, ok := a.cmdMap[subName]; ok {
				err := cmd.Run(ctx, []string{"-h"}, stdout, stderr)
				if errors.Is(err, flag.ErrHelp) {
					return nil
				}

				return err
			}

			return fmt.Errorf("unknown command %q for help. Run '%s help' for available commands", subName, a.Name)
		}

		a.PrintUsage(stdout)

		return nil

	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "%s version %s %s/%s\n", a.Name, a.Version, runtime.GOOS, runtime.GOARCH)
		return nil
	}

	// Direct match against registered command or alias
	if cmd, ok := a.cmdMap[first]; ok {
		err := cmd.Run(ctx, args[1:], stdout, stderr)
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	// If argument looks like a flag or file path, route to default "autopilot" command
	if strings.HasPrefix(first, "-") || strings.HasSuffix(first, ".go") || strings.Contains(first, "/") ||
		strings.Contains(first, `\`) || first == "." || first == "..." {
		if a.defaultCmd != nil {
			err := a.defaultCmd.Run(ctx, args, stdout, stderr)
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}

			return err
		}
	}

	return fmt.Errorf("unknown command %q. Run '%s help' for available commands", first, a.Name)
}

// PrintUsage writes formatted help instructions to the target writer.
func (a *App) PrintUsage(w io.Writer) {
	fmt.Fprintf(w, "%s %s — %s\n\n", a.Name, a.Version, a.Description)
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s <command> [flags] [arguments...]\n\n", a.Name)

	cmdByName := make(map[string]Command, len(a.Commands))
	for _, c := range a.Commands {
		cmdByName[c.Name()] = c
	}

	for _, group := range commandGroups {
		fmt.Fprintf(w, "%s:\n", group.Title)

		for _, name := range group.Commands {
			if c, ok := cmdByName[name]; ok {
				fmt.Fprintf(w, "  %-12s %s\n", c.Name(), c.Synopsis())
			}
		}

		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Global Flags:\n")
	fmt.Fprintf(w, "  -h, --help    Show help for any command or vortex itself\n")
	fmt.Fprintf(w, "  -v, --version Show vortex version and build information\n\n")

	fmt.Fprintf(w, "Quick Start Examples:\n")
	fmt.Fprintf(w, "  vortex                                 # Auto-detect, validate, and compile all contracts\n")
	fmt.Fprintf(w, "  vortex gen ./pkg/api/api.go            # Compile zero-alloc client for specific file\n")
	fmt.Fprintf(w, "  vortex mock ./pkg/api/api.go           # Generate in-memory mock server for tests\n")
	fmt.Fprintf(w, "  vortex check ./...                     # Lint and typecheck all contracts\n")
	fmt.Fprintf(w, "  vortex diff ./openapi.json             # Detect breaking drift against OpenAPI spec\n")
	fmt.Fprintf(w, "  vortex record -out=app.har -- ./mycli  # Capture process-isolated live traffic\n")
	fmt.Fprintf(w, "  vortex oapi import -spec=app.har -add  # Ingest HAR into Go contract with 3-way merge\n\n")

	fmt.Fprintf(w, "Run '%s help <command>' or '%s <command> --help' for detailed documentation.\n", a.Name, a.Name)
}
