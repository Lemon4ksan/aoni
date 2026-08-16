// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// vortex is the official zero-allocation AST-driven code generator and OpenAPI 3.1 toolchain for Aoni.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/lemon4ksan/aoni/internal/version"
)

func main() {
	commands := []Command{
		&CmdAutoPilot{},
		&CmdStatus{},
		&CmdInit{},
		&CmdConfig{},
		&CmdWork{},
		&CmdGen{},
		&CmdHarness{},
		&CmdMock{},
		&CmdClean{},
		&CmdCheck{},
		&CmdDiff{},
		&CmdReview{},
		&CmdAccept{},
		&CmdCherryPick{},
		&CmdRefactor{},
		&CmdSource{},
		&CmdLog{},
		&CmdTag{},
		&CmdBlame{},
		&CmdOAPI{},
		&CmdProto{},
		&CmdBench{},
		&CmdProf{},
		&CmdCover{},
		&CmdList{},
		&CmdExplain{},
		&CmdExample{},
		&CmdPGO{},
		&CmdCompletion{},
	}

	app := NewApp(
		"vortex",
		version.Current,
		"Unified Zero-Allocation AST Toolchain and Engine Suite for projects using aoni",
		commands...,
	)

	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vortex: %v\n", err)
		os.Exit(1)
	}
}
