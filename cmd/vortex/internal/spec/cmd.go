// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spec

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/traffic"
)

// Cmd provides bidirectional OpenAPI 3.1, Protobuf, and Swagger schema import/export capabilities.
type Cmd struct{}

// NewCommand returns a new spec command instance.
func NewCommand() base.Command {
	return &Cmd{}
}

func (c *Cmd) Name() string      { return "spec" }
func (c *Cmd) Aliases() []string { return []string{"oapi"} }
func (c *Cmd) Synopsis() string {
	return "OpenAPI 3.1, Protobuf, and remote schema toolchain hub (import/export/diff/proto)"
}
func (c *Cmd) Usage() string { return "vortex spec <import|export|diff|proto|source> [flags]" }

func (c *Cmd) printUsage(w io.Writer) {
	fmt.Fprintf(w, "vortex spec — OpenAPI 3.1 & Schema Toolchain Hub\n\n")
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  vortex spec <import|export|diff|proto|source> [flags]\n\n")
	fmt.Fprintf(w, "Subcommands:\n")
	fmt.Fprintf(w, "  import    Import OpenAPI/Swagger or HAR traffic into Go contract with 3-way AST merge\n")
	fmt.Fprintf(w, "  export    Export @aoni:service Go contracts into OpenAPI 3.1 specifications\n")
	fmt.Fprintf(w, "  diff      Compare schema contracts against remote or reference specifications\n")
	fmt.Fprintf(w, "  proto     Compile Protocol Buffers with zero-allocation vtprotobuf codecs\n")
	fmt.Fprintf(w, "  source    Manage and synchronize remote upstream API specifications\n\n")
	fmt.Fprintf(w, "Examples:\n")
	fmt.Fprintf(w, "  vortex spec import -spec=openapi.json -out=./pkg/api/api.go\n")
	fmt.Fprintf(w, "  vortex spec import -spec=session.har -out=./pkg/api/api.go -add\n")
	fmt.Fprintf(w, "  vortex spec export -file=./pkg/api/api.go -out=openapi.json\n\n")
	fmt.Fprintf(w, "Run 'vortex spec import -h' or 'vortex spec export -h' for subcommand options.\n")
}

func (c *Cmd) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		c.printUsage(stderr)
		return nil
	}

	mode := strings.ToLower(args[0])
	switch mode {
	case "export":
		return c.runExport(ctx, args[1:], stdout, stderr)
	case "import":
		return c.runImport(ctx, args[1:], stdout, stderr)
	case "diff", "compare":
		return (&traffic.CmdDiff{}).Run(ctx, args[1:], stdout, stderr)
	case "proto", "protobuf":
		return (&CmdProto{}).Run(ctx, args[1:], stdout, stderr)
	case "source", "src", "upstream":
		return (&CmdSource{}).Run(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		c.printUsage(stdout)
		return nil
	default:
		c.printUsage(stderr)

		return fmt.Errorf(
			"unknown spec subcommand %q. Valid modes: 'import', 'export', 'diff', 'proto', 'source'",
			mode,
		)
	}
}
