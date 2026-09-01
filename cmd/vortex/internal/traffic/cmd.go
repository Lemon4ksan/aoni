// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package traffic

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
)

// Cmd provides a unified hub for traffic recording, inspection, drift detection, and credential vault.
type Cmd struct{}

// NewCommand returns a new traffic hub command instance.
func NewCommand() base.Command {
	return &Cmd{}
}

func (c *Cmd) Name() string      { return "traffic" }
func (c *Cmd) Aliases() []string { return nil }
func (c *Cmd) Synopsis() string {
	return "Manage and inspect local traffic captures, secrets vault, and live HAR recording"
}

func (c *Cmd) Usage() string {
	return "vortex traffic [record|inspect|diff|list|store|move|sanitize|export|secrets|prune] [flags]"
}

func (c *Cmd) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	args = base.NormalizeArgs(args)

	if len(args) == 0 {
		return c.printHelp(stderr)
	}

	sub := strings.ToLower(args[0])
	subArgs := args[1:]

	switch sub {
	case "record", "sniff", "capture":
		return (&CmdRecord{}).Run(ctx, subArgs, stdout, stderr)

	case "diff", "compare", "drift":
		return (&CmdDiff{}).Run(ctx, subArgs, stdout, stderr)

	case "list", "ls":
		return c.runList(ctx, subArgs, stdout, stderr)

	case "show", "inspect", "view", "dump":
		return c.runShow(ctx, subArgs, stdout, stderr)

	case "store", "add", "put", "save":
		return c.runStore(ctx, subArgs, stdout, stderr)

	case "move", "mv", "ingest", "absorb":
		return c.runMove(ctx, subArgs, stdout, stderr)

	case "sanitize", "clean-har":
		return c.runSanitize(ctx, subArgs, stdout, stderr)

	case "export", "restore":
		return c.runExport(ctx, subArgs, stdout, stderr)

	case "secrets", "vault", "secret":
		return c.runSecrets(ctx, subArgs, stdout, stderr)

	case "delete", "rm", "remove":
		return c.runDelete(ctx, subArgs, stdout, stderr)

	case "prune", "clean":
		return c.runPrune(ctx, subArgs, stdout, stderr)

	case "help", "-h", "--help":
		return c.printHelp(stderr)

	default:
		// Default to inspect if args look like a session ID, hash, or HAR file
		return c.runShow(ctx, args, stdout, stderr)
	}
}

func (c *Cmd) printHelp(stderr io.Writer) error {
	fmt.Fprintf(stderr, "vortex traffic — Network Traffic & Reverse Engineering Hub\n\n")
	fmt.Fprintf(stderr, "Usage:\n")
	fmt.Fprintf(stderr, "  vortex traffic record [flags] [-- cmd]           Capture live HTTP/HTTPS traffic\n")
	fmt.Fprintf(stderr, "  vortex traffic inspect <session|file.har>        Inspect HTTP requests in table view\n")
	fmt.Fprintf(stderr, "  vortex traffic diff <sessionA> <sessionB>        Detect parameter and schema deltas\n")
	fmt.Fprintf(stderr, "  vortex traffic list                              List all recorded traffic sessions\n")
	fmt.Fprintf(stderr, "  vortex traffic store <files...>                  Archive HAR files into traffic store\n")
	fmt.Fprintf(stderr, "  vortex traffic move <files...>                   Ingest HAR files and delete originals\n")
	fmt.Fprintf(stderr, "  vortex traffic sanitize <file> -out=<clean>      Export scrubbed, Git-safe HAR\n")
	fmt.Fprintf(stderr, "  vortex traffic export <id|hash> -out=<file>      Restore uncompressed HAR from store\n")
	fmt.Fprintf(stderr, "  vortex traffic secrets [list|get|set|clear]      Manage local credentials vault\n")
	fmt.Fprintf(stderr, "  vortex traffic delete <id|hash...>               Delete specific traffic sessions\n")
	fmt.Fprintf(stderr, "  vortex traffic prune [--all]                     Clean up expired/unused traffic\n\n")

	return nil
}
