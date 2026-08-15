// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/spec"
)

func runList(scopeFilter string, asJSON, asMarkdown bool) {
	if asJSON {
		data, err := json.MarshalIndent(spec.Registry, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "aoni-gen: error serializing spec: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(string(data))

		return
	}

	if asMarkdown {
		fmt.Print(spec.GenerateMarkdownTable())
		return
	}

	targetScope := spec.Scope(strings.ToLower(strings.TrimSpace(scopeFilter)))

	scopes := spec.Scopes()
	if targetScope != "" {
		scopes = []spec.Scope{targetScope}
	}

	totalDirectives := 0

	fmt.Println("aoni-gen DSL Directives Reference")
	fmt.Println("=================================")
	fmt.Println()

	for _, s := range scopes {
		directives := spec.ByScope(s)
		if len(directives) == 0 {
			continue
		}

		sort.Slice(directives, func(i, j int) bool {
			return directives[i].Name < directives[j].Name
		})

		title := string(s)
		if len(title) > 0 {
			title = strings.ToUpper(title[:1]) + title[1:]
		}

		fmt.Printf("[%s Scope Directives]\n", title)
		fmt.Printf("  %-20s %-32s %s\n", "DIRECTIVE", "ARGUMENTS / VALUE", "DESCRIPTION")
		fmt.Printf("  %-20s %-32s %s\n", "---------", "-----------------", "-----------")

		for _, d := range directives {
			totalDirectives++

			dirName := "@" + d.Name

			argsStr := make([]string, 0, len(d.Args)+1)
			if d.ValueHint != "" {
				argsStr = append(argsStr, d.ValueHint)
			}

			for _, arg := range d.Args {
				item := arg.Name
				if arg.Required {
					item += "*"
				}

				argsStr = append(argsStr, item)
			}

			argsCol := strings.Join(argsStr, ", ")
			if argsCol == "" {
				argsCol = "—"
			}

			if len(argsCol) > 30 {
				argsCol = argsCol[:27] + "..."
			}

			fmt.Printf("  %-20s %-32s %s\n", dirName, argsCol, d.Description)
		}

		fmt.Println()
	}

	fmt.Printf("Total: %d directive(s). Run 'aoni-gen explain <directive>' for details.\n", totalDirectives)
}

func runExplain(name string) {
	d := spec.Lookup(name)
	if d == nil {
		fmt.Fprintf(os.Stderr, "aoni-gen: unknown directive %q\n\n", name)
		fmt.Fprintf(os.Stderr, "Run 'aoni-gen list' to see all available directives.\n")
		os.Exit(1)
	}

	fmt.Println("================================================================================")

	aliasStr := ""
	if len(d.Aliases) > 0 {
		aliasStr = fmt.Sprintf(" (alias: @%s)", strings.Join(d.Aliases, ", @"))
	}

	fmt.Printf("  Directive: @%s%s\n", d.Name, aliasStr)

	scopes := make([]string, 0, len(d.Scopes))
	for _, s := range d.Scopes {
		scopes = append(scopes, string(s))
	}

	fmt.Printf("  Scope:     %s\n", strings.Join(scopes, ", "))
	fmt.Println("================================================================================")
	fmt.Println()

	fmt.Println("DESCRIPTION:")
	fmt.Printf("  %s\n\n", d.Description)

	if d.ValueHint != "" && len(d.Args) == 0 {
		fmt.Println("SYNTAX / VALUE:")
		fmt.Printf("  %s\n\n", d.ValueHint)
	} else if len(d.Args) > 0 {
		fmt.Println("ARGUMENTS:")

		if d.ValueHint != "" {
			fmt.Printf("  Value: %s\n\n", d.ValueHint)
		}

		for _, a := range d.Args {
			meta := make([]string, 0, 2)
			if a.Required {
				meta = append(meta, "required")
			} else {
				meta = append(meta, "optional")
			}

			if a.Default != "" {
				meta = append(meta, fmt.Sprintf("default: %q", a.Default))
			}

			placeholder := a.Placeholder
			if placeholder == "" {
				if len(a.AllowedValues) > 0 {
					placeholder = "<value>"
				} else {
					placeholder = "\"<value>\""
				}
			}

			fmt.Printf("  • %s=%s  [%s]\n", a.Name, placeholder, strings.Join(meta, ", "))
			fmt.Printf("    %s\n", a.Description)

			if len(a.AllowedValues) > 0 {
				fmt.Printf("    Allowed: %s\n", strings.Join(a.AllowedValues, " | "))
			}

			fmt.Println()
		}
	}

	fmt.Println("EXAMPLE:")

	lines := strings.Split(d.Example, "\n")
	for _, l := range lines {
		fmt.Printf("  %s\n", l)
	}

	fmt.Println()
}
