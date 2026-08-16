// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
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
		a.PrintUsage(stdout)
		return nil

	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "%s version %s %s/%s\n", a.Name, a.Version, runtime.GOOS, runtime.GOARCH)
		return nil
	}

	// Direct match against registered command or alias
	if cmd, ok := a.cmdMap[first]; ok {
		return cmd.Run(ctx, args[1:], stdout, stderr)
	}

	// If argument looks like a flag or file path, route to default "gen" command
	if strings.HasPrefix(first, "-") || strings.HasSuffix(first, ".go") || strings.Contains(first, "/") ||
		strings.Contains(first, `\`) || first == "." || first == "..." {
		if a.defaultCmd != nil {
			return a.defaultCmd.Run(ctx, args, stdout, stderr)
		}
	}

	return fmt.Errorf("unknown command %q. Run '%s help' for available commands", first, a.Name)
}

// PrintUsage writes formatted help instructions to the target writer.
func (a *App) PrintUsage(w io.Writer) {
	fmt.Fprintf(w, "%s %s — %s\n\n", a.Name, a.Version, a.Description)
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s [command] [flags] [arguments...]\n\n", a.Name)
	fmt.Fprintf(w, "Available Commands:\n")

	for _, c := range a.Commands {
		fmt.Fprintf(w, "  %-12s %s\n", c.Name(), c.Synopsis())
	}

	fmt.Fprintf(w, "\nRun '%s <command> -h' for command-specific flags and examples.\n", a.Name)
}
