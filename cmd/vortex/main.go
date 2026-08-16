// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// vortex is the official zero-allocation AST-driven code generator and OpenAPI 3.1 toolchain for Aoni.
package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	commands := []Command{
		&CmdGen{},
		&CmdCheck{},
		&CmdOAPI{},
		&CmdProto{},
		&CmdBench{},
		&CmdCover{},
		&CmdList{},
		&CmdExplain{},
		&CmdExample{},
	}

	app := NewApp(
		"vortex",
		"v0.6.0",
		"Unified Zero-Allocation AST Toolchain and Engine Suite for projects using aoni",
		commands...,
	)

	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vortex: %v\n", err)
		os.Exit(1)
	}
}
