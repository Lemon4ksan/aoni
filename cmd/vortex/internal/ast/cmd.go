// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ast

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
)

// Cmd provides AST-level contract restructuring, renaming, version journals, and history.
type Cmd struct{}

// NewCommand returns a new ast command instance.
func NewCommand() base.Command {
	return &Cmd{}
}

func (c *Cmd) Name() string      { return "ast" }
func (c *Cmd) Aliases() []string { return []string{"refactor"} }
func (c *Cmd) Synopsis() string {
	return "Refactor contracts via AST: tuple deobfuscation, interface splitting, batch rename, and history"
}

func (c *Cmd) Usage() string {
	return "vortex ast [tuple|split|rename|pick|review|accept|undo|blame|history|log|tag|stack] [contract] [flags]"
}

func (c *Cmd) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "review", "audit":
			return (&CmdReview{}).Run(ctx, args[1:], stdout, stderr)
		case "accept", "merge":
			return (&CmdAccept{}).Run(ctx, args[1:], stdout, stderr)
		case "undo", "revert":
			return (&CmdUndo{}).Run(ctx, args[1:], stdout, stderr)
		case "blame", "provenance":
			return (&CmdBlame{}).Run(ctx, args[1:], stdout, stderr)
		case "history", "journal":
			return (&CmdHistory{}).Run(ctx, args[1:], stdout, stderr)
		case "pick", "cherry-pick", "cp", "transplant":
			return (&CmdCherryPick{}).Run(ctx, args[1:], stdout, stderr)
		case "log", "timeline":
			return (&CmdLog{}).Run(ctx, args[1:], stdout, stderr)
		case "tag", "release":
			return (&CmdTag{}).Run(ctx, args[1:], stdout, stderr)
		case "stack":
			return (&CmdStack{}).Run(ctx, args[1:], stdout, stderr)
		}
	}

	fs := flag.NewFlagSet("ast", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fromFlag    string
		toFlag      string
		methodsFlag string
		matchFlag   string
		replaceFlag string
		typeFlag    string
		fieldFlag   string
		outFlag     string
		dryRunFlag  bool
		genFlag     bool
		dirFlag     string
		jsFlag      string
	)

	base.StringVar(fs, &fromFlag, "from", "", "", "Source interface name or file path to refactor from")
	base.StringVar(fs, &toFlag, "to", "", "", "Target interface name or new field name")
	base.StringVar(
		fs,
		&methodsFlag,
		"methods",
		"m",
		"",
		"Glob pattern or comma-separated list of methods to split (e.g. 'Get*,List*')",
	)
	base.StringVar(fs, &matchFlag, "match", "", "", "Regex match pattern for renaming (e.g. 'Fetch(.*)')")
	base.StringVar(fs, &replaceFlag, "replace", "", "", "Replacement pattern for renaming (e.g. 'Get$1')")
	base.StringVar(
		fs,
		&typeFlag,
		"type",
		"t",
		"",
		"Target struct/tuple type name to rename field in (e.g. 'GenerateContentRequest')",
	)
	base.StringVar(fs, &fieldFlag, "field", "f", "", "Target field name or positional tag index (e.g. 'Field4' or '4')")
	base.StringVar(fs, &outFlag, "out", "o", "", "Output file for split interface (default: same file)")
	base.BoolVar(fs, &dryRunFlag, "dry-run", "", false, "Preview AST refactor without writing changes to disk")
	base.BoolVar(fs, &genFlag, "gen", "", true, "Automatically re-generate API clients after refactoring")
	base.StringVar(fs, &dirFlag, "dir", "", "", "Target workspace directory (default: current root)")
	base.StringVar(
		fs,
		&jsFlag,
		"js",
		"",
		"",
		"JavaScript bundle files or glob patterns for schema extraction (e.g. '*.js')",
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex ast — AST Contract Refactoring & Tuple Deobfuscation\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex ast tuple [file.go|contract] [--js=\"*.js\"] [--dry-run]\n")
		fmt.Fprintf(
			stderr,
			"  vortex ast split --from=<Interface> --methods=\"Get*,List*\" --to=<NewInterface> [--out=path.go]\n",
		)
		fmt.Fprintf(stderr, "  vortex ast rename --match=\"Fetch(.*)\" --replace=\"Get$1\" [file.go|contract]\n")
		fmt.Fprintf(
			stderr,
			"  vortex ast rename --type=<Struct> --field=<FieldOrTag> --to=<NewName> [file.go|contract]\n\n",
		)
		fmt.Fprintf(stderr, "Subcommands:\n")
		fmt.Fprintf(stderr, "  tuple        Deobfuscate positional tuple indices into named semantic fields\n")
		fmt.Fprintf(stderr, "  split        Split large interface into focused sub-interfaces\n")
		fmt.Fprintf(stderr, "  rename       Batch rename methods, types, or fields across contracts\n")
		fmt.Fprintf(stderr, "  pick         Cherry-pick methods and DTOs across contracts\n")
		fmt.Fprintf(stderr, "  history      Display immutable version history journal of contracts\n")
		fmt.Fprintf(stderr, "  blame        Inspect field-level provenance and traffic origins\n")
		fmt.Fprintf(stderr, "  undo         Revert contract AST to a previous journal revision\n")
		fmt.Fprintf(stderr, "  log          Display Git history of API contracts\n")
		fmt.Fprintf(stderr, "  tag          Create semantic release tags for contracts\n")
		fmt.Fprintf(stderr, "  review       Review pending contract proposals\n")
		fmt.Fprintf(stderr, "  accept       Merge reviewed contract proposals into main contract\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	rt, err := base.NewRuntime(dirFlag)
	if err != nil {
		return err
	}

	action := "split"

	var targetArg string

	if len(posArgs) > 0 {
		first := strings.ToLower(posArgs[0])
		switch first {
		case "tuple", "tuples":
			action = "tuple"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}

		case "enum", "enums":
			action = "enum"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}

		case "split":
			action = "split"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}

		case "rename":
			action = "rename"

			if len(posArgs) > 1 {
				targetArg = posArgs[1]
			}

		default:
			if matchFlag != "" {
				action = "rename"
			}

			targetArg = posArgs[0]
		}
	}

	switch action {
	case "enum":
		target := targetArg
		if target == "" {
			target = fromFlag
		}

		return c.runEnum(ctx, rt, target, dryRunFlag, genFlag, stdout, stderr)

	case "tuple":
		target := targetArg
		if target == "" {
			target = fromFlag
		}

		return c.runTuple(ctx, rt, target, jsFlag, dryRunFlag, genFlag, stdout, stderr)

	case "rename":
		target := targetArg
		if target == "" {
			target = fromFlag
		}

		return c.runRename(
			ctx,
			rt,
			target,
			matchFlag,
			replaceFlag,
			typeFlag,
			fieldFlag,
			toFlag,
			dryRunFlag,
			genFlag,
			stdout,
			stderr,
		)

	case "split":
		if fromFlag == "" && targetArg != "" {
			fromFlag = targetArg
		}

		return c.runSplit(ctx, rt, fromFlag, toFlag, methodsFlag, outFlag, dryRunFlag, genFlag, stdout, stderr)

	default:
		return fmt.Errorf(
			"unknown ast action %q (use 'tuple', 'split', 'rename', 'pick', 'history', 'undo', 'blame')",
			action,
		)
	}
}
