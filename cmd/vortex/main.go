// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// vortex is the official zero-allocation AST-driven code generator and OpenAPI 3.1 toolchain for Aoni.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/ast"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/core"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/oracle"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/perf"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/spec"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/traffic"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/workspace"
	"github.com/lemon4ksan/aoni/internal/version"
)

// DefaultCommands returns the isolated canonical command set for the Vortex CLI.
func DefaultCommands(runner base.AppRunner) []base.Command {
	cmds := make([]base.Command, 0, 25)

	// Daily Core Commands
	cmds = append(cmds, core.Commands(runner)...)

	// Domain Hubs
	cmds = append(cmds, traffic.NewCommand())
	cmds = append(cmds, oracle.NewCommand())
	cmds = append(cmds, spec.NewCommand())
	cmds = append(cmds, ast.NewCommand())
	cmds = append(cmds, perf.NewCommand())

	// Top-level shortcuts for frequent dev workflows
	cmds = append(cmds, &perf.CmdBench{})
	cmds = append(cmds, &perf.CmdCover{})

	// Workspace Management
	cmds = append(cmds, workspace.Commands(runner)...)

	return cmds
}

func main() {
	app := NewApp(
		"vortex",
		version.Current,
		"Unified Zero-Allocation AST Toolchain and Engine Suite for projects using aoni",
	)

	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vortex: %v\n", err)
		os.Exit(1)
	}
}
