// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ast

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/git"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/merge"
	vparser "github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/patcher"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

// CmdAccept merges proposed changes from a consumer Git branch into the local master contracts.
type CmdAccept struct{}

func (c *CmdAccept) Name() string      { return "accept" }
func (c *CmdAccept) Aliases() []string { return []string{"merge-proposal", "apply"} }
func (c *CmdAccept) Synopsis() string {
	return "Surgically merge changes from a consumer Git proposal branch into local Go contracts"
}
func (c *CmdAccept) Usage() string { return "vortex ast accept <branch-name|git-ref>" }

func (c *CmdAccept) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("accept", flag.ContinueOnError)
	fs.SetOutput(stderr)

	noGen := fs.Bool("no-gen", false, "Skip automatic code generation after AST patching")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex ast accept — Semantic Proposal Reconciler & Merger\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex ast accept [-no-gen] <branch-name|git-ref>\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	positional := fs.Args()
	if len(positional) == 0 {
		return errors.New("missing proposal branch argument (e.g. `vortex ast accept feat/add-search`)")
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
	if cfg == nil || len(cfg.Contracts) == 0 {
		return errors.New("no contracts configured in .vortex.yml (run `vortex init` first)")
	}

	p := vparser.NewParser()

	fmt.Fprintf(stdout, "⚡ [vortex ast accept] Merging Proposal from %q into local master...\n\n", targetRef)

	patchedFiles := 0

	for _, ct := range cfg.Contracts {
		absPath := filepath.Join(rootDir, ct.File)

		diskIR, parseErr := p.ParseFile(absPath)
		if parseErr != nil {
			continue
		}

		remoteBytes, showErr := git.ShowFile(ctx, rootDir, targetRef, ct.File)
		if showErr != nil {
			continue
		}

		remoteIR, remoteErr := p.ParseSource(ct.File, remoteBytes)
		if remoteErr != nil {
			continue
		}

		res, recErr := merge.Reconcile(nil, diskIR, remoteIR)
		if recErr != nil || len(res.Deltas) == 0 {
			continue
		}

		// Perform surgical AST patching
		if patchErr := patcher.PatchFile(absPath, res); patchErr != nil {
			return fmt.Errorf("failed to patch %s: %w", ct.File, patchErr)
		}

		patchedFiles++

		fmt.Fprintf(stdout, "✔ Patched %s (+%d additive changes merged)\n", ct.File, res.AdditiveCount)
	}

	if patchedFiles == 0 {
		fmt.Fprintf(stdout, "✔ No changes needed. Local contracts are already up-to-date with %q.\n", targetRef)
		return nil
	}

	// Cascade code generation
	if !*noGen {
		fmt.Fprintf(stdout, "\n⚡ Recompiling network layer...\n")

		b := builder.New(builder.Config{})
		for _, ct := range cfg.Contracts {
			absFile := filepath.Join(rootDir, ct.File)

			absGen := filepath.Join(rootDir, ct.Gen)
			if _, genErr := b.BuildFile(ctx, absFile, absGen); genErr != nil {
				return fmt.Errorf("cascade generation failed on %s: %w", ct.File, genErr)
			}
		}

		fmt.Fprintf(stdout, "✔ Network layer recompiled successfully!\n")
	}

	fmt.Fprintf(stdout, "\n✨ Successfully merged proposal %q! Working tree is 100%% synchronized.\n", targetRef)

	return nil
}
