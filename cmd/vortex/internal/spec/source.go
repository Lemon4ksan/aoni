// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spec

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/pathkit"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/builder"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/diff"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ingest"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/openapi"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

// CmdSource manages upstream API specification sources and schemas.
type CmdSource struct{}

func (c *CmdSource) Name() string      { return "source" }
func (c *CmdSource) Aliases() []string { return []string{"src", "upstream", "remote"} }
func (c *CmdSource) Synopsis() string {
	return "Manage, fetch, diff, and synchronize upstream API specifications (OpenAPI, Swagger, AsyncAPI)"
}

func (c *CmdSource) Usage() string {
	return "vortex spec source [list|set|rm|fetch|diff|ping|sync] [contract] [source_url_or_path] [flags]"
}

func (c *CmdSource) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("source", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dirFlag      = fs.String("dir", "", "Target workspace directory (default: current root)")
		formatFlag   = fs.String("format", "", "Schema format: openapi, swagger, asyncapi, proto, raw")
		fetchFlag    = fs.Bool("fetch", false, "Immediately fetch remote spec to api/specs/")
		outFlag      = fs.String("out", "", "Destination file for fetched specification")
		refreshFlag  = fs.String("refresh", "", "Command to refresh upstream schema dump")
		generateFlag = fs.String("generate", "", "Command to generate Go contract from schema dump")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex spec source — Manage upstream API specifications and remote schemas\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(
			stderr,
			"  vortex spec source [list]                                   List all configured upstream sources\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex spec source set <contract> <url_or_path> [--fetch]   Bind an upstream specification to contract\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex spec source rm <contract>                            Unbind upstream source from contract\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex spec source fetch [contract]                         Download remote specifications locally\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex spec source ping [contract]                          Verify reachability of remote endpoints\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex spec source diff [contract]                          Compare Go contract against upstream schema\n",
		)
		fmt.Fprintf(
			stderr,
			"  vortex spec source sync [contract]                          Fetch and re-generate API clients\n\n",
		)
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	targetDir := *dirFlag
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	cfg, err := project.Load(targetDir)
	if err != nil {
		return fmt.Errorf("loading workspace configuration: %w", err)
	}

	action := "list"

	var contractArg, sourceArg string

	if len(posArgs) > 0 {
		first := strings.ToLower(posArgs[0])
		switch first {
		case "list", "ls":
			action = "list"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

		case "set", "add":
			action = "set"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

			if len(posArgs) > 2 {
				sourceArg = posArgs[2]
			}

		case "rm", "unset", "remove", "del", "delete":
			action = "rm"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

		case "fetch", "pull", "get":
			action = "fetch"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

		case "ping", "check":
			action = "ping"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

		case "diff":
			action = "diff"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

		case "sync", "reconcile", "update":
			action = "sync"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

		default:
			// If first positional argument matches a contract name, treat as "diff" or "list"
			contractArg = posArgs[0]
			if len(posArgs) > 1 {
				action = "set"
				sourceArg = posArgs[1]
			} else {
				action = "list"
			}
		}
	}

	switch action {
	case "list":
		return c.runList(stdout, cfg, contractArg)
	case "set":
		return c.runSet(
			ctx,
			stdout,
			cfg,
			contractArg,
			sourceArg,
			*formatFlag,
			*refreshFlag,
			*generateFlag,
			*fetchFlag,
			*outFlag,
		)

	case "rm":
		return c.runRm(stdout, cfg, contractArg)
	case "fetch":
		return c.runFetch(ctx, stdout, cfg, contractArg, *outFlag)
	case "ping":
		return c.runPing(ctx, stdout, cfg, contractArg)
	case "diff":
		return c.runDiff(ctx, stdout, cfg, contractArg)
	case "sync":
		return c.runSync(ctx, stdout, cfg, contractArg, *outFlag)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func (c *CmdSource) findContractIndex(cfg *project.Config, target string) int {
	for i, ct := range cfg.Contracts {
		if strings.EqualFold(ct.Name, target) || strings.EqualFold(ct.Package, target) ||
			strings.EqualFold(ct.File, target) || strings.HasSuffix(ct.File, "/"+target) ||
			strings.HasSuffix(ct.File, "\\"+target) {
			return i
		}
	}

	return -1
}

func (c *CmdSource) runList(stdout io.Writer, cfg *project.Config, target string) error {
	fmt.Fprintf(stdout, "⚡ Vortex Upstream Sources (%s)\n\n", cfg.RootDir)

	contracts := cfg.Contracts
	if target != "" {
		idx := c.findContractIndex(cfg, target)
		if idx == -1 {
			return fmt.Errorf("contract %q not found in workspace", target)
		}

		contracts = []project.ContractConfig{cfg.Contracts[idx]}
	}

	if len(contracts) == 0 {
		fmt.Fprintf(stdout, "No contracts registered in .vortex.yml. Run `vortex init` first.\n")
		return nil
	}

	fmt.Fprintf(stdout, "%-16s %-10s %-45s %s\n", "CONTRACT", "FORMAT", "SOURCE", "FILE")
	fmt.Fprintf(stdout, "%s\n", strings.Repeat("─", 90))

	for _, ct := range contracts {
		sourceStr := "(none)"
		formatStr := "-"

		if ct.Upstream != nil && ct.Upstream.Source != "" {
			sourceStr = ct.Upstream.Source

			formatStr = ct.Upstream.Format
			if formatStr == "" {
				formatStr = "openapi"
			}
		}

		fmt.Fprintf(stdout, "%-16s %-10s %-45s %s\n", ct.Name, formatStr, truncateString(sourceStr, 44), ct.File)
	}

	return nil
}

func (c *CmdSource) runSet(
	ctx context.Context,
	stdout io.Writer,
	cfg *project.Config,
	contractName, source, format, refresh, generate string,
	doFetch bool,
	outDest string,
) error {
	if contractName == "" || source == "" {
		return errors.New("usage: vortex spec source set <contract> <url_or_file>")
	}

	idx := c.findContractIndex(cfg, contractName)
	if idx == -1 {
		return fmt.Errorf("contract %q not found in workspace", contractName)
	}

	ct := &cfg.Contracts[idx]

	if format == "" {
		if strings.HasSuffix(source, ".json") || strings.HasSuffix(source, ".yaml") ||
			strings.HasSuffix(source, ".yml") {
			format = "openapi"
		}
	}

	ct.Upstream = &project.UpstreamConfig{
		Source:   source,
		Format:   format,
		Refresh:  refresh,
		Generate: generate,
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Successfully bound upstream source to %s:\n", ct.Name)
	fmt.Fprintf(stdout, "  • Source: %s\n", source)

	if format != "" {
		fmt.Fprintf(stdout, "  • Format: %s\n", format)
	}

	if doFetch {
		fmt.Fprintf(stdout, "\n")
		return c.runFetch(ctx, stdout, cfg, ct.Name, outDest)
	}

	return nil
}

func (c *CmdSource) runRm(stdout io.Writer, cfg *project.Config, contractName string) error {
	if contractName == "" {
		return errors.New("usage: vortex spec source rm <contract>")
	}

	idx := c.findContractIndex(cfg, contractName)
	if idx == -1 {
		return fmt.Errorf("contract %q not found in workspace", contractName)
	}

	ct := &cfg.Contracts[idx]
	ct.Upstream = nil

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Removed upstream source binding from %s\n", ct.Name)

	return nil
}

func (c *CmdSource) runFetch(ctx context.Context, stdout io.Writer, cfg *project.Config, target, outDest string) error {
	var targets []project.ContractConfig
	if target != "" {
		idx := c.findContractIndex(cfg, target)
		if idx == -1 {
			return fmt.Errorf("contract %q not found in workspace", target)
		}

		targets = []project.ContractConfig{cfg.Contracts[idx]}
	} else {
		for _, ct := range cfg.Contracts {
			if ct.Upstream != nil && ct.Upstream.Source != "" {
				targets = append(targets, ct)
			}
		}
	}

	if len(targets) == 0 {
		fmt.Fprintf(stdout, "No upstream sources configured to fetch.\n")
		return nil
	}

	client := &http.Client{Timeout: 15 * time.Second}

	for _, ct := range targets {
		if ct.Upstream == nil || ct.Upstream.Source == "" {
			continue
		}

		srcPath := pathkit.New(ct.Upstream.Source)
		src := srcPath.String()
		if srcPath.IsFile() {
			// Local file - verify existence
			localPath := srcPath.FilePath()
			if !srcPath.IsAbs() {
				localPath = filepath.Join(cfg.RootDir, localPath)
			}

			if fi, err := os.Stat(localPath); err == nil {
				fmt.Fprintf(stdout, "✔ Verified local schema %s (%s, %.1f KB)\n",
					ct.Name, src, float64(fi.Size())/1024)
			} else {
				fmt.Fprintf(stdout, "❌ Local schema file not found for %s: %s\n", ct.Name, localPath)
			}

			continue
		}

		// Remote URL
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			fmt.Fprintf(stdout, "❌ Invalid URL for %s (%s): %v\n", ct.Name, src, err)
			continue
		}

		req.Header.Set("User-Agent", "Vortex-Schema-Client/1.0")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(stdout, "❌ Failed fetching %s from %s: %v\n", ct.Name, src, err)
			continue
		}

		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			fmt.Fprintf(stdout, "❌ Failed reading body for %s: %v\n", ct.Name, err)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			fmt.Fprintf(stdout, "❌ HTTP %d from %s\n", resp.StatusCode, src)
			continue
		}

		// Destination path
		savePath := outDest
		if savePath == "" {
			savePath = filepath.Join(cfg.RootDir, "api", "specs", strings.ToLower(ct.Name)+".json")
		} else if !filepath.IsAbs(savePath) {
			savePath = filepath.Join(cfg.RootDir, savePath)
		}

		if err := os.MkdirAll(filepath.Dir(savePath), 0o750); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}

		if err := os.WriteFile(savePath, data, 0o600); err != nil {
			return fmt.Errorf("writing spec file: %w", err)
		}

		relSave, _ := filepath.Rel(cfg.RootDir, savePath)
		fmt.Fprintf(stdout, "✔ Fetched %s schema -> %s (HTTP %d, %.1f KB)\n",
			ct.Name, filepath.ToSlash(relSave), resp.StatusCode, float64(len(data))/1024)
	}

	return nil
}

func (c *CmdSource) runPing(ctx context.Context, stdout io.Writer, cfg *project.Config, target string) error {
	var targets []project.ContractConfig
	if target != "" {
		idx := c.findContractIndex(cfg, target)
		if idx == -1 {
			return fmt.Errorf("contract %q not found in workspace", target)
		}

		targets = []project.ContractConfig{cfg.Contracts[idx]}
	} else {
		for _, ct := range cfg.Contracts {
			if ct.Upstream != nil && ct.Upstream.Source != "" {
				targets = append(targets, ct)
			}
		}
	}

	if len(targets) == 0 {
		fmt.Fprintf(stdout, "No upstream sources configured.\n")
		return nil
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, ct := range targets {
		srcPath := pathkit.New(ct.Upstream.Source)
		src := srcPath.String()
		if srcPath.IsFile() {
			localPath := srcPath.FilePath()
			if !srcPath.IsAbs() {
				localPath = filepath.Join(cfg.RootDir, localPath)
			}

			if fi, err := os.Stat(localPath); err == nil {
				fmt.Fprintf(
					stdout,
					"✔ %-14s [LOCAL]    %s (%.1f KB)\n",
					ct.Name,
					src,
					float64(fi.Size())/1024,
				)
			} else {
				fmt.Fprintf(stdout, "❌ %-14s [LOCAL]    File not found: %s\n", ct.Name, localPath)
			}

			continue
		}

		start := time.Now()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		req.Header.Set("User-Agent", "Vortex-Schema-Client/1.0")

		resp, err := client.Do(req)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Fprintf(stdout, "❌ %-14s [UNREACH]  %s (%v)\n", ct.Name, src, err)
			continue
		}

		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			fmt.Fprintf(
				stdout,
				"✔ %-14s [HTTP %d]  %s (RTT: %v)\n",
				ct.Name,
				resp.StatusCode,
				truncateString(src, 50),
				elapsed.Round(time.Millisecond),
			)
		} else {
			fmt.Fprintf(stdout, "🟡 %-14s [HTTP %d]  %s\n", ct.Name, resp.StatusCode, truncateString(src, 50))
		}
	}

	return nil
}

func (c *CmdSource) runDiff(ctx context.Context, stdout io.Writer, cfg *project.Config, target string) error {
	var targets []project.ContractConfig
	if target != "" {
		idx := c.findContractIndex(cfg, target)
		if idx == -1 {
			return fmt.Errorf("contract %q not found in workspace", target)
		}

		targets = []project.ContractConfig{cfg.Contracts[idx]}
	} else {
		for _, ct := range cfg.Contracts {
			if ct.Upstream != nil && ct.Upstream.Source != "" {
				targets = append(targets, ct)
			}
		}
	}

	if len(targets) == 0 {
		fmt.Fprintf(stdout, "No upstream sources configured to diff. Bind a source with `vortex spec source set`.\n")
		return nil
	}

	p := parser.NewParser()

	for _, ct := range targets {
		if ct.Upstream == nil || ct.Upstream.Source == "" {
			continue
		}

		goPath := filepath.Join(cfg.RootDir, ct.File)

		root, err := p.ParseFile(goPath)
		if err != nil {
			fmt.Fprintf(stdout, "❌ Parsing Go contract %s: %v\n", ct.File, err)
			continue
		}

		pSrc := pathkit.New(ct.Upstream.Source)
		srcPath := pSrc.String()
		if pSrc.IsFile() && !pSrc.IsAbs() {
			srcPath = filepath.Join(cfg.RootDir, pSrc.FilePath())
		}

		rawBytes, readErr := readSourceBytes(ctx, srcPath)
		if readErr != nil {
			fmt.Fprintf(stdout, "❌ Reading spec for %s (%s): %v\n", ct.Name, ct.Upstream.Source, readErr)
			continue
		}

		format, _ := ingest.DetectFormat(rawBytes)
		if format == ingest.FormatOpenAPI3 || format == ingest.FormatSwagger2 {
			doc, docErr := openapi.LoadSpec(srcPath, rawBytes)
			if docErr != nil {
				fmt.Fprintf(stdout, "❌ Parsing schema for %s: %v\n", ct.Name, docErr)
				continue
			}

			report := diff.Compare(root, doc, ct.File, ct.Upstream.Source)
			fmt.Fprintf(stdout, "\n● Contract %s (%s vs %s):\n", ct.Name, ct.File, ct.Upstream.Source)
			fmt.Fprint(stdout, report.Render(true))
		} else {
			fmt.Fprintf(stdout, "● Contract %s: format %s (semantic diff not applicable)\n", ct.Name, format)
		}
	}

	return nil
}

func (c *CmdSource) runSync(ctx context.Context, stdout io.Writer, cfg *project.Config, target, outDest string) error {
	var targets []project.ContractConfig
	if target != "" {
		idx := c.findContractIndex(cfg, target)
		if idx == -1 {
			return fmt.Errorf("contract %q not found in workspace", target)
		}

		targets = []project.ContractConfig{cfg.Contracts[idx]}
	} else {
		for _, ct := range cfg.Contracts {
			if ct.Upstream != nil && ct.Upstream.Source != "" {
				targets = append(targets, ct)
			}
		}
	}

	if len(targets) == 0 {
		fmt.Fprintf(stdout, "No upstream sources configured to sync.\n")
		return nil
	}

	// 1. Fetch remote specs if needed
	if err := c.runFetch(ctx, stdout, cfg, target, outDest); err != nil {
		return err
	}

	// 2. Re-generate API clients
	var files []string
	for _, ct := range targets {
		files = append(files, filepath.Join(cfg.RootDir, ct.File))
	}

	b := builder.New(builder.Config{})

	results, err := b.BuildFiles(ctx, files)
	if err != nil {
		return fmt.Errorf("re-generating clients: %w", err)
	}

	for _, res := range results {
		if !res.Skipped {
			relOut, _ := filepath.Rel(cfg.RootDir, res.OutputFile)
			fmt.Fprintf(stdout, "✔ Synchronized client -> %s (%d bytes)\n", filepath.ToSlash(relOut), res.BytesCount)
		}
	}

	return nil
}

func readSourceBytes(ctx context.Context, src string) ([]byte, error) {
	p := pathkit.New(src)
	if p.IsURL() {
		client := &http.Client{Timeout: 10 * time.Second}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.String(), nil)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		return io.ReadAll(resp.Body)
	}

	return os.ReadFile(p.FilePath())
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return s[:maxLen]
	}

	return s[:maxLen-3] + "..."
}
