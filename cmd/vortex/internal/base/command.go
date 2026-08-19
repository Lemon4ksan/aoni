// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package base

import (
	"context"
	"io"
)

// Command represents an isolated Vortex CLI subcommand.
type Command interface {
	// Name returns the primary subcommand invocation name (e.g. "check", "gen", "bench").
	Name() string
	// Aliases returns alternative command names (e.g. "lint" for "check").
	Aliases() []string
	// Synopsis returns a one-line summary for help listings.
	Synopsis() string
	// Usage returns detailed syntax instructions.
	Usage() string
	// Run executes the command logic with isolated arguments and streams.
	Run(ctx context.Context, args []string, stdout, stderr io.Writer) error
}

// AppRunner represents an interface to execute subcommands or run workflows from orchestrator commands.
type AppRunner interface {
	Run(ctx context.Context, args []string) error
	RunCommand(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error
	PrintUsage(w io.Writer)
}
