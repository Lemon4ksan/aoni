// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ast

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/git"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// CmdLog renders an in-source API evolution timeline from @version, @since, and @deprecated tags, or real Git history.
type CmdLog struct{}

func (c *CmdLog) Name() string      { return "log" }
func (c *CmdLog) Aliases() []string { return []string{"timeline"} }
func (c *CmdLog) Synopsis() string {
	return "Display contract evolution timeline from in-source tags or Git history"
}
func (c *CmdLog) Usage() string { return "vortex ast log [flags] [--git] [-n=10] [files...]" }

type timelineVersion struct {
	Version    string   `json:"version"`
	Source     string   `json:"source,omitempty"`
	Added      []string `json:"added"`
	Deprecated []string `json:"deprecated"`
}

type fileTimeline struct {
	FilePath string            `json:"file_path"`
	Service  string            `json:"service"`
	Versions []timelineVersion `json:"versions"`
}

func (c *CmdLog) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		jsonFlag  = fs.Bool("json", false, "Output timeline in JSON format")
		gitFlag   = fs.Bool("git", false, "Show actual Git commit history for contracts")
		limitFlag = fs.Int("n", 10, "Maximum number of Git commits to display")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex ast log — Contract Evolution Timeline Explorer\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex ast log [-json] [paths...]\n")
		fmt.Fprintf(stderr, "  vortex ast log --git [-n=10] [paths...]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	files := builder.CollectInputFiles("", fs.Args())
	if len(files) == 0 {
		return errors.New("no Go source files found to inspect for timeline")
	}

	if *gitFlag {
		return c.runGitLog(ctx, files, *limitFlag, *jsonFlag, stdout)
	}

	p := codeparser.NewParser()

	var allTimelines []fileTimeline

	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if strings.HasSuffix(file, ".gen.go") {
			continue
		}

		root, err := p.ParseFile(file)
		if err != nil || len(root.Services) == 0 {
			continue
		}

		for _, svc := range root.Services {
			timeline := c.buildServiceTimeline(file, svc)
			allTimelines = append(allTimelines, timeline)
		}
	}

	if len(allTimelines) == 0 {
		return errors.New("no @aoni:service contracts found in specified files")
	}

	if *jsonFlag {
		jsonBytes, err := json.MarshalIndent(allTimelines, "", "  ")
		if err != nil {
			return fmt.Errorf("failed formatting JSON: %w", err)
		}

		fmt.Fprintln(stdout, string(jsonBytes))

		return nil
	}

	for _, tl := range allTimelines {
		fmt.Fprintf(stdout, "⚡ Vortex API Contract Timeline: %s (%s)\n\n", tl.FilePath, tl.Service)

		for _, v := range tl.Versions {
			sourceSuffix := ""
			if v.Source != "" {
				sourceSuffix = fmt.Sprintf(" (source: %s)", v.Source)
			}

			fmt.Fprintf(stdout, "● %s%s\n", v.Version, sourceSuffix)

			if len(v.Added) > 0 {
				fmt.Fprintf(stdout, "  [+] %d endpoint(s) introduced:\n", len(v.Added))

				for _, ep := range v.Added {
					fmt.Fprintf(stdout, "      • %s\n", ep)
				}
			}

			if len(v.Deprecated) > 0 {
				fmt.Fprintf(stdout, "  [-] %d endpoint(s) deprecated:\n", len(v.Deprecated))

				for _, ep := range v.Deprecated {
					fmt.Fprintf(stdout, "      • %s\n", ep)
				}
			}

			fmt.Fprintf(stdout, "\n")
		}
	}

	return nil
}

func (c *CmdLog) runGitLog(ctx context.Context, files []string, limit int, jsonOut bool, stdout io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	rootDir, err := git.RootDir(ctx, cwd)
	if err != nil {
		return err
	}

	for _, file := range files {
		relPath, relErr := filepath.Rel(rootDir, file)
		if relErr != nil {
			relPath = file
		}

		commits, logErr := git.LogCommits(ctx, rootDir, relPath, limit)
		if logErr != nil {
			continue
		}

		if jsonOut {
			jsonBytes, _ := json.MarshalIndent(commits, "", "  ")
			fmt.Fprintln(stdout, string(jsonBytes))
			continue
		}

		fmt.Fprintf(stdout, "⚡ Vortex API Git History: %s\n\n", relPath)

		for _, commit := range commits {
			shortHash := commit.Hash
			if len(shortHash) > 7 {
				shortHash = shortHash[:7]
			}

			fmt.Fprintf(stdout, "● commit %s (%s) by @%s\n", shortHash, commit.Date, commit.Author)
			fmt.Fprintf(stdout, "  %s\n\n", commit.Subject)
		}
	}

	return nil
}

func (c *CmdLog) buildServiceTimeline(filePath string, svc *ir.ServiceIR) fileTimeline {
	timeline := fileTimeline{
		FilePath: filePath,
		Service:  svc.Name,
		Versions: make([]timelineVersion, 0),
	}

	versionMap := make(map[string]*timelineVersion)

	serviceVersion := svc.Version
	if serviceVersion == "" {
		serviceVersion = "v1.0.0"
	}

	for _, m := range svc.Methods {
		v := m.Since
		if v == "" {
			v = m.Version
		}

		if v == "" {
			v = serviceVersion
		}

		tv, exists := versionMap[v]
		if !exists {
			tv = &timelineVersion{
				Version:    v,
				Source:     svc.Source,
				Added:      make([]string, 0),
				Deprecated: make([]string, 0),
			}
			versionMap[v] = tv
		}

		rawPath := ""
		if m.Path != nil {
			rawPath = m.Path.RawTemplate
		}

		endpointStr := fmt.Sprintf("%s %s (%s)", strings.ToUpper(m.HTTPMethod), rawPath, m.Name)

		if m.Deprecation != nil {
			reason := ""
			if m.Deprecation.Reason != "" {
				reason = " — " + m.Deprecation.Reason
			}

			tv.Deprecated = append(tv.Deprecated, endpointStr+reason)
		} else {
			tv.Added = append(tv.Added, endpointStr)
		}
	}

	timeline.Versions = generic.Map(generic.Values(versionMap), func(tv *timelineVersion) timelineVersion {
		return *tv
	})

	slices.SortFunc(timeline.Versions, func(a, b timelineVersion) int {
		return cmp.Compare(b.Version, a.Version)
	})

	return timeline
}
