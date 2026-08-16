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
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/diff"
	"github.com/lemon4ksan/aoni/internal/codegen/git"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/merge"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdDiff compares local Go interface contracts against an external OpenAPI specification or Git ref.
type CmdDiff struct{}

func (c *CmdDiff) Name() string      { return "diff" }
func (c *CmdDiff) Aliases() []string { return []string{"drift", "compare"} }
func (c *CmdDiff) Synopsis() string {
	return "Detect contract drift between local Go interfaces and OpenAPI specifications or Git refs"
}

func (c *CmdDiff) Usage() string {
	return "vortex diff [flags] [--against=<ref>] [<remote-spec.json|yaml>] [local-files...]"
}

func (c *CmdDiff) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		failOnDriftFlag = fs.Bool(
			"fail-on-drift",
			false,
			"Exit with non-zero code if breaking contract drift is detected",
		)
		strictFlag = fs.Bool(
			"strict",
			false,
			"Exit with non-zero code on any drift (including non-breaking and ghosts)",
		)
		jsonFlag    = fs.Bool("json", false, "Output report in JSON format")
		serviceFlag = fs.String("service", "", "Filter comparison to a specific service interface name")
		specFlag    = fs.String("spec", "", "Path to remote OpenAPI/Swagger JSON or YAML specification")
		againstFlag = fs.String(
			"against",
			"",
			"Compare local Go contracts against a Git branch, tag, or commit in-memory (e.g. --against=origin/main)",
		)
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex diff — Contract Drift Inspector\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(
			stderr,
			"  vortex diff [-fail-on-drift] [-strict] [-json] [-service=name] <spec.json|yaml> [paths...]\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex diff --against=<branch|tag|commit> [paths...]\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	positional := fs.Args()

	// Branch 1: Git-based comparison
	if *againstFlag != "" {
		return c.runGitDiff(ctx, *againstFlag, positional, *jsonFlag, stdout)
	}

	// Branch 2: OpenAPI-based comparison
	specFile := *specFlag

	var localPaths []string

	if specFile == "" {
		if len(positional) == 0 {
			return errors.New(
				"missing OpenAPI specification or --against flag (e.g. `vortex diff swagger.json` or `vortex diff --against=origin/main`)",
			)
		}

		specFile = positional[0]
		localPaths = positional[1:]
	} else {
		localPaths = positional
	}

	// 1. Load remote OpenAPI specification
	doc, err := openapi.LoadSpec(specFile, nil)
	if err != nil {
		return fmt.Errorf("failed loading OpenAPI spec %q: %w", specFile, err)
	}

	// 2. Collect and parse local Go contract files
	files := builder.CollectInputFiles("", localPaths)
	if len(files) == 0 {
		return errors.New("no Go source files found to compare against specification")
	}

	p := codeparser.NewParser()

	var (
		allServices []*ir.ServiceIR
		allStructs  []*ir.StructIR
	)

	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if strings.HasSuffix(file, ".gen.go") {
			continue
		}

		root, parseErr := p.ParseFile(file)
		if parseErr != nil {
			continue
		}

		for _, s := range root.Services {
			if *serviceFlag != "" && !strings.EqualFold(s.Name, *serviceFlag) {
				continue
			}

			allServices = append(allServices, s)
		}

		allStructs = append(allStructs, root.Structs...)
	}

	if len(allServices) == 0 {
		return errors.New("no matching service interfaces found in local Go files")
	}

	localRoot := &ir.RootIR{
		Services: allServices,
		Structs:  allStructs,
	}

	localDesc := strings.Join(files, ", ")
	if len(files) > 3 {
		localDesc = fmt.Sprintf("%d files (%s...)", len(files), filepath.Base(files[0]))
	}

	// 3. Run semantic diff engine
	engine := diff.NewEngine()
	report := engine.Compare(localRoot, doc, localDesc, filepath.Base(specFile))

	// 4. Render output
	if *jsonFlag {
		jsonBytes, renderErr := report.RenderJSON()
		if renderErr != nil {
			return fmt.Errorf("failed formatting JSON report: %w", renderErr)
		}

		fmt.Fprintln(stdout, string(jsonBytes))
	} else {
		fmt.Fprint(stdout, report.Render(true))
	}

	// 5. Check exit constraints
	if *strictFlag && report.HasDrift() {
		return fmt.Errorf("contract drift detected under strict mode (%d issue(s))", len(report.Drifts))
	}

	if *failOnDriftFlag && report.HasBreaking() {
		return fmt.Errorf("breaking contract drift detected (%d breaking issue(s))", report.BreakingCount())
	}

	return nil
}

func (c *CmdDiff) runGitDiff(
	ctx context.Context,
	targetRef string,
	paths []string,
	jsonOut bool,
	stdout io.Writer,
) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	rootDir, err := git.RootDir(ctx, cwd)
	if err != nil {
		return err
	}

	files := builder.CollectInputFiles("", paths)
	if len(files) == 0 {
		if cfg, _ := project.Load(rootDir); cfg != nil && len(cfg.Contracts) > 0 {
			for _, ct := range cfg.Contracts {
				files = append(files, filepath.Join(rootDir, ct.File))
			}
		}
	}

	if len(files) == 0 {
		return errors.New("no Go contract files found to compare")
	}

	p := codeparser.NewParser()
	reconciler := merge.NewReconciler()

	fmt.Fprintf(stdout, "⚡ [vortex diff] Comparing working tree against '%s':\n\n", targetRef)

	totalDeltas := 0

	for _, file := range files {
		relPath, relErr := filepath.Rel(rootDir, file)
		if relErr != nil {
			relPath = file
		}

		diskIR, parseErr := p.ParseFile(file)
		if parseErr != nil {
			continue
		}

		remoteBytes, showErr := git.ShowFile(ctx, rootDir, targetRef, relPath)
		if showErr != nil {
			continue
		}

		remoteIR, remoteErr := p.ParseSource(relPath, remoteBytes)
		if remoteErr != nil {
			continue
		}

		res, recErr := reconciler.Reconcile(nil, diskIR, remoteIR)
		if recErr != nil || len(res.Deltas) == 0 {
			continue
		}

		fmt.Fprintf(stdout, "● %s:\n", relPath)

		for _, d := range res.Deltas {
			totalDeltas++

			prefix := "[+]"
			switch d.Kind {
			case merge.DeltaModifyMethod, merge.DeltaModifyStruct:
				prefix = "[~]"
			case merge.DeltaDeprecate:
				prefix = "[-]"
			}

			fmt.Fprintf(stdout, "  %s %s: %s\n", prefix, d.EntityName, d.Description)
		}

		fmt.Fprintf(stdout, "\n")
	}

	if totalDeltas == 0 {
		fmt.Fprintf(stdout, "✔ Working tree is in sync with '%s' (0 drift).\n", targetRef)
	}

	return nil
}
