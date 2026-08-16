// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/lemon4ksan/aoni/internal/codegen/spec"
)

// CmdExample scaffolds ready-made contract templates.
type CmdExample struct{}

func (c *CmdExample) Name() string { return "example" }
func (c *CmdExample) Aliases() []string {
	return []string{"examples", "template", "templates", "init"}
}
func (c *CmdExample) Synopsis() string { return "Scaffold ready-made declarative contract templates" }
func (c *CmdExample) Usage() string    { return "vortex example [http|ws|socket|pipeline] [flags]" }

func (c *CmdExample) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("example", flag.ContinueOnError)
	fs.SetOutput(stderr)

	outFile := fs.String("out", "", "Write template source code to file")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex example — Scaffold Ready-Made Contract Templates\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex example [http|ws|socket|pipeline] [-out=api.go]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	kind := ""
	if len(fs.Args()) > 0 {
		kind = fs.Args()[0]
	}

	if kind == "" || kind == "list" || kind == "help" {
		fmt.Fprint(stdout, spec.PrintExampleHelp())
		return nil
	}

	ex := spec.LookupExample(kind)
	if ex == nil {
		fmt.Fprintf(stderr, "vortex example: unknown template kind %q\n\n", kind)
		fmt.Fprint(stderr, spec.PrintExampleHelp())
		return fmt.Errorf("unknown template kind %q", kind)
	}

	if *outFile != "" {
		if err := os.WriteFile(*outFile, []byte(ex.SourceCode), 0o600); err != nil {
			return fmt.Errorf("failed writing %s: %w", *outFile, err)
		}

		fmt.Fprintf(stdout, "✔ Successfully created %s template (%s) in %s\n", ex.Kind, ex.Title, *outFile)

		return nil
	}

	fmt.Fprint(stdout, ex.SourceCode)

	return nil
}
