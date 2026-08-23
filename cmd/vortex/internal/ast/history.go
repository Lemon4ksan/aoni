// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ast

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/history"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

// CmdHistory inspects the operation history journal.
type CmdHistory struct{}

func (c *CmdHistory) Name() string      { return "history" }
func (c *CmdHistory) Aliases() []string { return []string{"ops", "operations", "journal"} }
func (c *CmdHistory) Synopsis() string {
	return "View AST mutation journal and operation history for undo/revert"
}

func (c *CmdHistory) Usage() string {
	return "vortex ast history [--json] [flags]"
}

func (c *CmdHistory) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dirFlag  = fs.String("dir", "", "Target workspace directory (default: current root)")
		jsonFlag = fs.Bool("json", false, "Output history in JSON format")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex ast history — AST Operation Journal & History\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex ast history [--json]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex ast history\n")
		fmt.Fprintf(stderr, "  vortex ast undo         # Revert the latest operation\n")
		fmt.Fprintf(stderr, "  vortex ast undo op-a1b2 # Revert a specific operation\n\n")
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

	entries, err := history.List(targetDir)
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	if *jsonFlag {
		data, jErr := json.MarshalIndent(entries, "", "  ")
		if jErr != nil {
			return jErr
		}

		fmt.Fprintln(stdout, string(data))

		return nil
	}

	fmt.Fprintf(stdout, "⚡ Vortex Operation History Journal (%s)\n\n", targetDir)

	if len(entries) == 0 {
		fmt.Fprintf(stdout, "No operations recorded yet. All modifying operations are automatically tracked here.\n")
		return nil
	}

	fmt.Fprintf(stdout, "  %-10s %-16s %-42s %s\n", "OP-ID", "TIME", "COMMAND", "MODIFIED FILES")
	fmt.Fprintf(stdout, "  %s\n", strings.Repeat("─", 95))

	for _, e := range entries {
		timeStr := e.CreatedAt.Format("15:04:05")
		ago := time.Since(e.CreatedAt).Round(time.Second)
		timeCol := fmt.Sprintf("%s (%s ago)", timeStr, ago)

		statusMarker := ""
		if e.Undone {
			statusMarker = " [UNDONE]"
		}

		filesStr := strings.Join(e.Files, ", ")
		fmt.Fprintf(
			stdout,
			"  %-10s %-16s %-42s %s%s\n",
			e.ID,
			truncateString(timeCol, 15),
			truncateString(e.Command, 41),
			truncateString(filesStr, 25),
			statusMarker,
		)
	}

	fmt.Fprintf(stdout, "\nTip: Run `vortex ast undo` to revert the latest operation.\n")

	return nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return s[:maxLen]
	}

	return s[:maxLen-3] + "..."
}
