// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ast

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/history"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdUndo reverts the latest contract modifying operation or a specific op-id.
type CmdUndo struct{}

func (c *CmdUndo) Name() string      { return "undo" }
func (c *CmdUndo) Aliases() []string { return []string{"revert", "rollback", "pop"} }
func (c *CmdUndo) Synopsis() string {
	return "Undo the last contract modification, refactor, or cherry-pick operation"
}

func (c *CmdUndo) Usage() string {
	return "vortex ast undo [op-id] [flags]"
}

func (c *CmdUndo) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("undo", flag.ContinueOnError)
	fs.SetOutput(stderr)

	dirFlag := fs.String("dir", "", "Target workspace directory (default: current root)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex ast undo — AST Operation Rollback & Revert Tool\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex ast undo [op-id]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex ast undo         # Revert the latest modification\n")
		fmt.Fprintf(stderr, "  vortex ast undo op-a1b2 # Revert a specific operation\n")
		fmt.Fprintf(stderr, "  vortex ast history      # View list of reversible operations\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if arg == "-dir" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			nonFlags = append(nonFlags, arg)
		}
	}

	if err := fs.Parse(append(flags, nonFlags...)); err != nil {
		return err
	}

	targetDir := *dirFlag
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	opID := ""
	if len(fs.Args()) > 0 {
		opID = fs.Args()[0]
	}

	entry, err := history.Undo(ctx, targetDir, opID)
	if err != nil {
		return fmt.Errorf("undo failed: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Successfully reverted %s\n", entry.ID)
	fmt.Fprintf(stdout, "  • Original Command: %s\n", entry.Command)
	fmt.Fprintf(stdout, "  • Restored Files:   %s\n", strings.Join(entry.Files, ", "))
	fmt.Fprintf(stdout, "  • Re-generated:     API clients updated\n")

	return nil
}
