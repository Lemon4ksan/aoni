// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// CmdLog renders an in-source API evolution timeline from @version, @since, and @deprecated tags.
type CmdLog struct{}

func (c *CmdLog) Name() string      { return "log" }
func (c *CmdLog) Aliases() []string { return []string{"timeline", "history"} }
func (c *CmdLog) Synopsis() string {
	return "Display contract evolution timeline from in-source @version, @since, and @deprecated tags"
}
func (c *CmdLog) Usage() string { return "vortex log [flags] [files...]" }

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

	jsonFlag := fs.Bool("json", false, "Output timeline in JSON format")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex log — Contract Evolution Timeline Explorer\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex log [-json] [paths...]\n\n")
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
			return fmt.Errorf("formatting JSON timeline: %w", err)
		}

		fmt.Fprintln(stdout, string(jsonBytes))

		return nil
	}

	for _, tl := range allTimelines {
		fmt.Fprintf(stdout, "⚡ Vortex API Contract Timeline: %s (%s)\n\n", tl.FilePath, tl.Service)

		for _, v := range tl.Versions {
			versionTitle := v.Version
			if versionTitle == "" {
				versionTitle = "Unversioned / Initial Contract"
			}

			if v.Source != "" {
				fmt.Fprintf(stdout, "● %s (source: %s)\n", versionTitle, v.Source)
			} else {
				fmt.Fprintf(stdout, "● %s\n", versionTitle)
			}

			if len(v.Added) > 0 {
				fmt.Fprintf(stdout, "  [+] %d endpoint(s) introduced:\n", len(v.Added))

				for _, a := range v.Added {
					fmt.Fprintf(stdout, "      • %s\n", a)
				}
			}

			if len(v.Deprecated) > 0 {
				fmt.Fprintf(stdout, "  [!] %d endpoint(s) deprecated:\n", len(v.Deprecated))

				for _, d := range v.Deprecated {
					fmt.Fprintf(stdout, "      • %s\n", d)
				}
			}

			fmt.Fprintln(stdout)
		}
	}

	return nil
}

func (c *CmdLog) buildServiceTimeline(filePath string, svc *ir.ServiceIR) fileTimeline {
	tl := fileTimeline{
		FilePath: filePath,
		Service:  svc.Name,
	}

	versionMap := make(map[string]*timelineVersion)
	ensureVersion := func(ver string) *timelineVersion {
		if v, ok := versionMap[ver]; ok {
			return v
		}

		v := &timelineVersion{
			Version:    ver,
			Added:      make([]string, 0),
			Deprecated: make([]string, 0),
		}
		versionMap[ver] = v

		return v
	}

	// Service base version
	if svc.Version != "" {
		v := ensureVersion(svc.Version)
		if svc.Source != "" {
			v.Source = svc.Source
		}
	}

	for _, m := range svc.Methods {
		routeDesc := m.Name
		if m.HTTPMethod != "" && m.Path != nil {
			routeDesc = fmt.Sprintf("%s %s (%s)", strings.ToUpper(m.HTTPMethod), m.Path.RawTemplate, m.Name)
		}

		if m.Deprecation != nil {
			depVer := m.Deprecation.Since
			if depVer == "" {
				depVer = svc.Version
			}

			v := ensureVersion(depVer)

			msg := routeDesc
			if m.Deprecation.Reason != "" {
				msg += " — " + m.Deprecation.Reason
			}

			v.Deprecated = append(v.Deprecated, msg)
		}

		sinceVer := m.Since
		if sinceVer == "" {
			sinceVer = svc.Version
		}

		v := ensureVersion(sinceVer)
		v.Added = append(v.Added, routeDesc)
	}

	// Sort versions descending
	sortedVersions := make([]timelineVersion, 0, len(versionMap))
	for _, v := range versionMap {
		sortedVersions = append(sortedVersions, *v)
	}

	sort.Slice(sortedVersions, func(i, j int) bool {
		return sortedVersions[i].Version > sortedVersions[j].Version
	})

	tl.Versions = sortedVersions

	return tl
}
