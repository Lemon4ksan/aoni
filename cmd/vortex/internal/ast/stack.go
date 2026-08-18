// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ast

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/diff"
)

// CmdStack provides snapshot stacking, stepping, and step-by-step AST evolution comparisons.
type CmdStack struct{}

func (c *CmdStack) Name() string      { return "stack" }
func (c *CmdStack) Aliases() []string { return []string{"snap", "checkpoint"} }
func (c *CmdStack) Synopsis() string {
	return "Snapshot stack management for step-by-step reverse engineering, AST evolution, and diff inspection"
}

func (c *CmdStack) Usage() string {
	return "vortex ast stack <push|list|diff|pop|restore|clear> [flags] [args...]"
}

func (c *CmdStack) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		c.printUsage(stderr)
		return nil
	}

	subcmd := strings.ToLower(args[0])
	subArgs := args[1:]

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	stack, err := diff.LoadStack(cwd)
	if err != nil {
		return fmt.Errorf("initializing diff stack: %w", err)
	}

	switch subcmd {
	case "push", "save", "commit":
		return c.runPush(subArgs, stack, stdout, stderr)
	case "list", "ls", "status", "show":
		return c.runList(subArgs, stack, stdout, stderr)
	case "diff", "compare", "evolution":
		return c.runDiff(subArgs, stack, stdout, stderr)
	case "pop", "undo":
		return c.runPop(subArgs, stack, stdout, stderr)
	case "restore", "checkout", "jump":
		return c.runRestore(subArgs, stack, stdout, stderr)
	case "clear", "reset":
		return c.runClear(stack, stdout, stderr)
	case "help", "-h", "--help":
		c.printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown stack subcommand %q. Run 'vortex ast stack --help' for available commands", subcmd)
	}
}

func (c *CmdStack) runPush(args []string, stack *diff.DiffStack, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stack push", flag.ContinueOnError)
	fs.SetOutput(stderr)

	labelFlag := fs.String("label", "", "Human-readable label for this snapshot frame (e.g. 'deobf-models')")
	tagsFlag := fs.String("tags", "", "Comma-separated tags (e.g. 'tuples,methods,raw')")

	if err := fs.Parse(args); err != nil {
		return err
	}

	var tags []string
	if *tagsFlag != "" {
		for _, t := range strings.Split(*tagsFlag, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	filePaths := fs.Args()

	frame, err := stack.Push(*labelFlag, filePaths, tags, nil)
	if err != nil {
		return fmt.Errorf("pushing stack frame: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Captured stack snapshot frame #%d [%s] (%d files tracked)\n",
		frame.Index, frame.Label, len(frame.Files))

	if frame.Summary != "" {
		fmt.Fprintf(stdout, "  Delta vs previous: %s\n", frame.Summary)
	}

	return nil
}

func (c *CmdStack) runList(args []string, stack *diff.DiffStack, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stack list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonFlag := fs.Bool("json", false, "Output frames list in JSON format")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *jsonFlag {
		frames := stack.List()
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(frames)
	}

	fmt.Fprint(stdout, stack.RenderStackDiagram())

	return nil
}

func (c *CmdStack) runDiff(args []string, stack *diff.DiffStack, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stack diff", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonFlag := fs.Bool("json", false, "Output evolution report in JSON format")
	cumulativeFlag := fs.Bool("cumulative", false, "Compare current top frame against base frame #0")
	fromFlag := fs.String("from", "", "Source frame index or label")
	toFlag := fs.String("to", "", "Target frame index or label (defaults to current top)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	var (
		rep *diff.StackDiffResult
		err error
	)

	switch {
	case *cumulativeFlag:
		rep, err = stack.DiffCumulative()

	case *fromFlag != "" || *toFlag != "":
		from := *fromFlag
		if from == "" {
			from = "0"
		}

		to := *toFlag
		if to == "" {
			top := stack.Peek()
			if top != nil {
				to = strconv.Itoa(top.Index)
			}
		}

		rep, err = stack.DiffFrames(from, to)

	default:
		// Default: adjacent diff (Top vs Top-1)
		rep, err = stack.DiffAdjacent()
	}

	if err != nil {
		return fmt.Errorf("computing frame diff: %w", err)
	}

	if *jsonFlag {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}

	fmt.Fprint(stdout, rep.RenderText())

	return nil
}

func (c *CmdStack) runPop(args []string, stack *diff.DiffStack, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stack pop", flag.ContinueOnError)
	fs.SetOutput(stderr)

	restoreFlag := fs.Bool("restore", true, "Restore workspace files on disk to the new top frame state")
	if err := fs.Parse(args); err != nil {
		return err
	}

	popped, err := stack.Pop(*restoreFlag)
	if err != nil {
		return fmt.Errorf("popping frame: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Popped frame #%d [%s]\n", popped.Index, popped.Label)

	if *restoreFlag {
		top := stack.Peek()
		if top != nil {
			fmt.Fprintf(stdout, "  Restored disk state to frame #%d [%s]\n", top.Index, top.Label)
		} else {
			fmt.Fprintf(stdout, "  Stack is now empty.\n")
		}
	}

	return nil
}

func (c *CmdStack) runRestore(args []string, stack *diff.DiffStack, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stack restore", flag.ContinueOnError)
	fs.SetOutput(stderr)

	if err := fs.Parse(args); err != nil {
		return err
	}

	posArgs := fs.Args()
	if len(posArgs) == 0 {
		return errors.New("must specify target frame index, label, or 'base'")
	}

	target := posArgs[0]

	popped, err := stack.PopTo(target, true)
	if err != nil {
		return fmt.Errorf("restoring to frame %s: %w", target, err)
	}

	fmt.Fprintf(stdout, "✔ Restored workspace state to frame %q (popped %d newer frames)\n",
		target, len(popped))

	return nil
}

func (c *CmdStack) runClear(stack *diff.DiffStack, stdout, stderr io.Writer) error {
	if err := stack.Clear(); err != nil {
		return fmt.Errorf("clearing stack: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Cleared all frames from diff comparison stack\n")

	return nil
}

func (c *CmdStack) printUsage(w io.Writer) {
	fmt.Fprintf(w, "vortex ast stack — Snapshot comparison stack for step-by-step reverse engineering\n\n")
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  vortex ast stack push [flags] [files...]      Capture new snapshot frame\n")
	fmt.Fprintf(w, "  vortex ast stack list [--json]                Display ASCII stack tree diagram\n")
	fmt.Fprintf(w, "  vortex ast stack diff [--cumulative] [flags]  Diff AST tuple/method evolution\n")
	fmt.Fprintf(w, "  vortex ast stack pop [--restore]              Pop top frame\n")
	fmt.Fprintf(w, "  vortex ast stack restore <frame|label|base>   Rollback to target frame\n")
	fmt.Fprintf(w, "  vortex ast stack clear                        Clear all frames\n\n")
	fmt.Fprintf(w, "Examples:\n")
	fmt.Fprintf(w, "  vortex ast stack push -label=\"raw-ingest\"\n")
	fmt.Fprintf(w, "  vortex ast stack push -label=\"deobf-models\" -tags=\"tuples\"\n")
	fmt.Fprintf(w, "  vortex ast stack diff                         # Diff top vs previous frame\n")
	fmt.Fprintf(w, "  vortex ast stack diff --cumulative            # Cumulative evolution vs base\n")
	fmt.Fprintf(w, "  vortex ast stack restore base                 # Jump back to base\n")
}
