// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/spec"
)

// CmdExplain outputs in-depth syntax, argument specifications, and examples for a directive.
type CmdExplain struct{}

func (c *CmdExplain) Name() string      { return "explain" }
func (c *CmdExplain) Aliases() []string { return []string{"doc", "help-directive"} }
func (c *CmdExplain) Synopsis() string {
	return "Show documentation, syntax, and examples for a directive"
}
func (c *CmdExplain) Usage() string { return "vortex explain <directive>" }

func (c *CmdExplain) Run(_ context.Context, args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("directive name required (e.g. 'vortex explain form')")
	}

	name := args[0]

	d := spec.Lookup(name)
	if d == nil {
		return fmt.Errorf("unknown directive %q. Run 'vortex explain' to view available directives", name)
	}

	fmt.Fprintln(stdout, "================================================================================")

	aliasStr := ""
	if len(d.Aliases) > 0 {
		aliasStr = fmt.Sprintf(" (alias: @%s)", strings.Join(d.Aliases, ", @"))
	}

	fmt.Fprintf(stdout, "  Directive: @%s%s\n", d.Name, aliasStr)

	scopes := make([]string, 0, len(d.Scopes))
	for _, s := range d.Scopes {
		scopes = append(scopes, string(s))
	}

	fmt.Fprintf(stdout, "  Scope:     %s\n", strings.Join(scopes, ", "))
	fmt.Fprintln(stdout, "================================================================================")
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "DESCRIPTION:")
	fmt.Fprintf(stdout, "  %s\n\n", d.Description)

	if d.ValueHint != "" && len(d.Args) == 0 {
		fmt.Fprintln(stdout, "SYNTAX / VALUE:")
		fmt.Fprintf(stdout, "  %s\n\n", d.ValueHint)
	} else if len(d.Args) > 0 {
		fmt.Fprintln(stdout, "ARGUMENTS:")

		if d.ValueHint != "" {
			fmt.Fprintf(stdout, "  Value: %s\n\n", d.ValueHint)
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

			fmt.Fprintf(stdout, "  • %s=%s  [%s]\n", a.Name, placeholder, strings.Join(meta, ", "))
			fmt.Fprintf(stdout, "    %s\n", a.Description)

			if len(a.AllowedValues) > 0 {
				fmt.Fprintf(stdout, "    Allowed: %s\n", strings.Join(a.AllowedValues, " | "))
			}

			fmt.Fprintln(stdout)
		}
	}

	fmt.Fprintln(stdout, "EXAMPLE:")

	lines := strings.Split(d.Example, "\n")
	for _, l := range lines {
		fmt.Fprintf(stdout, "  %s\n", l)
	}

	fmt.Fprintln(stdout)

	return nil
}
