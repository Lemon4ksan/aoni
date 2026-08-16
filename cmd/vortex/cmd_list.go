// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/spec"
)

// CmdList displays the catalog of DSL directives and pipeline stages.
type CmdList struct{}

func (c *CmdList) Name() string { return "list" }
func (c *CmdList) Aliases() []string {
	return []string{"directives", "linters", "pipelines", "pipeline", "stages"}
}

func (c *CmdList) Synopsis() string {
	return "List all available @aoni DSL directives and pipeline stages"
}
func (c *CmdList) Usage() string { return "vortex list [flags]" }

func (c *CmdList) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		scopeFilter = fs.String(
			"scope",
			"",
			"Filter directives by scope (service, socket, method, param, struct, pipeline)",
		)
		asJSON     = fs.Bool("json", false, "Output list of directives as JSON")
		asMarkdown = fs.Bool("markdown", false, "Output list of directives as Markdown")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex list — List Available @aoni DSL Directives\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(
			stderr,
			"  vortex list [-scope=service|socket|method|param|struct|pipeline] [-json] [-markdown]\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *asJSON {
		data, err := json.MarshalIndent(spec.Registry, "", "  ")
		if err != nil {
			return fmt.Errorf("serializing spec to JSON: %w", err)
		}

		fmt.Fprintln(stdout, string(data))

		return nil
	}

	if *asMarkdown {
		fmt.Fprint(stdout, spec.GenerateMarkdownTable())
		return nil
	}

	targetScope := spec.Scope(strings.ToLower(strings.TrimSpace(*scopeFilter)))

	scopeTitles := map[spec.Scope]string{
		spec.ScopeService:  "◆ Service Scope (Interface Type Declarations)",
		spec.ScopeSocket:   "◆ Socket Scope (Real-Time WebSocket & Socket.IO Declarations)",
		spec.ScopeMethod:   "◆ Method Scope (Endpoint Routing & Wire Encoding)",
		spec.ScopeParam:    "◆ Param Scope (Parameter-Level Binding & Overrides)",
		spec.ScopeStruct:   "◆ Struct Scope (DTO Serialization & Tuples)",
		spec.ScopePipeline: "◆ Pipeline Stages (Wire-Transform Data Extraction & Decoders)",
	}

	scopes := spec.Scopes()
	if targetScope != "" {
		scopes = []spec.Scope{targetScope}
	}

	totalDirectives := 0

	fmt.Fprintln(stdout, "==========================================================================================")
	fmt.Fprintln(stdout, "                   vortex DSL Directives & Pipeline Reference                             ")
	fmt.Fprintln(stdout, "==========================================================================================")
	fmt.Fprintln(stdout)

	for _, s := range scopes {
		directives := spec.ByScope(s)
		if len(directives) == 0 {
			continue
		}

		sort.Slice(directives, func(i, j int) bool {
			return directives[i].Name < directives[j].Name
		})

		title := scopeTitles[s]
		if title == "" {
			title = fmt.Sprintf("◆ %s Scope", strings.ToUpper(string(s)[:1])+string(s)[1:])
		}

		fmt.Fprintln(stdout, title)
		fmt.Fprintln(stdout, strings.Repeat("─", 90))

		for _, d := range directives {
			totalDirectives++

			aliasStr := ""
			if len(d.Aliases) > 0 {
				aliasStr = fmt.Sprintf(" (aliases: @%s)", strings.Join(d.Aliases, ", @"))
			}

			fmt.Fprintf(stdout, "  @%-20s %s%s\n", d.Name, d.Description, aliasStr)

			if d.Example != "" {
				fmt.Fprintf(stdout, "  %-20s   ↳ %s\n", "", d.Example)
			}

			fmt.Fprintln(stdout)
		}
	}

	fmt.Fprintln(stdout, "==========================================================================================")
	fmt.Fprintf(
		stdout,
		"Total: %d directive(s). Run 'vortex explain <directive>' for full documentation & arguments.\n",
		totalDirectives,
	)
	fmt.Fprintln(stdout, "==========================================================================================")

	return nil
}
