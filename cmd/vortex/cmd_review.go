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
	"path/filepath"

	"github.com/lemon4ksan/aoni/internal/codegen/git"
	"github.com/lemon4ksan/aoni/internal/codegen/merge"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdReview audits incoming consumer proposal branches against local working contracts.
type CmdReview struct{}

func (c *CmdReview) Name() string      { return "review" }
func (c *CmdReview) Aliases() []string { return []string{"inspect-proposal", "audit"} }
func (c *CmdReview) Synopsis() string {
	return "Audit semantic network changes proposed by a consumer Git branch"
}
func (c *CmdReview) Usage() string { return "vortex review <branch-name|git-ref>" }

func (c *CmdReview) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex review — Semantic Proposal Auditor\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex review <branch-name|git-ref>\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	positional := fs.Args()
	if len(positional) == 0 {
		return errors.New(
			"missing proposal branch argument (e.g. `vortex review feat/add-search` or `vortex review origin/feat/user-avatar`)",
		)
	}

	targetRef := positional[0]

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	rootDir, err := git.RootDir(ctx, cwd)
	if err != nil {
		return err
	}

	cfg, _ := project.Load(rootDir)

	var contractFiles []string
	if cfg != nil && len(cfg.Contracts) > 0 {
		for _, ct := range cfg.Contracts {
			contractFiles = append(contractFiles, ct.File)
		}
	} else {
		return errors.New("no contracts configured in .vortex.yml (run `vortex init` first)")
	}

	p := codeparser.NewParser()
	reconciler := merge.NewReconciler()

	fmt.Fprintf(stdout, "⚡ [vortex review] Auditing Proposal from %q\n\n", targetRef)

	totalDeltas := 0

	for _, relPath := range contractFiles {
		absPath := filepath.Join(rootDir, relPath)

		diskIR, err := p.ParseFile(absPath)
		if err != nil {
			continue
		}

		remoteBytes, err := git.ShowFile(ctx, rootDir, targetRef, relPath)
		if err != nil {
			continue
		}

		remoteIR, err := p.ParseSource(relPath, remoteBytes)
		if err != nil {
			continue
		}

		res, err := reconciler.Reconcile(nil, diskIR, remoteIR)
		if err != nil {
			continue
		}

		if len(res.Deltas) == 0 {
			continue
		}

		fmt.Fprintf(stdout, "● Contract: %s (%s)\n", relPath, diskIR.PackageName)

		for _, delta := range res.Deltas {
			totalDeltas++

			prefix := "[+]"
			switch delta.Kind {
			case merge.DeltaModifyMethod, merge.DeltaModifyStruct:
				prefix = "[~]"
			case merge.DeltaDeprecate:
				prefix = "[-]"
			}

			fmt.Fprintf(stdout, "  %s %s: %s\n", prefix, delta.EntityName, delta.Description)
		}

		fmt.Fprintf(stdout, "\n")
	}

	if totalDeltas == 0 {
		fmt.Fprintf(stdout, "✔ No semantic differences found. Working copy and %q are identical.\n", targetRef)
		return nil
	}

	fmt.Fprintf(stdout, "───────────────────────────────────────────────────────────────────\n")
	fmt.Fprintf(stdout, "Next Actions:\n")
	fmt.Fprintf(stdout, "  ↳ Run `vortex accept %s` to merge proposed changes into Go master\n\n", targetRef)

	return nil
}
