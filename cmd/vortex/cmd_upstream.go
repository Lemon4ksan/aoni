// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdUpstream manages upstream reverse-engineering web bundles, OpenAPI/AsyncAPI schemas, and proto definitions.
type CmdUpstream struct{}

func (c *CmdUpstream) Name() string      { return "upstream" }
func (c *CmdUpstream) Aliases() []string { return []string{"up", "vendor"} }
func (c *CmdUpstream) Synopsis() string {
	return "Manage upstream reverse-engineering web bundles, schemas, and assets with auto Git PR collapsing"
}

func (c *CmdUpstream) Usage() string {
	return "vortex upstream <add|list|diff|sync> [source|url] [flags]"
}

func (c *CmdUpstream) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return c.runList(stdout, stderr)
	}

	subCmd := strings.ToLower(args[0])
	subArgs := args[1:]

	switch subCmd {
	case "add", "ingest", "store", "save":
		return c.runAdd(ctx, subArgs, stdout, stderr)
	case "list", "ls":
		return c.runList(stdout, stderr)
	case "sync", "fetch", "pull":
		return c.runSync(ctx, subArgs, stdout, stderr)
	default:
		// If first argument is a file path or URL, treat directly as "add"
		if strings.HasPrefix(subCmd, "http://") || strings.HasPrefix(subCmd, "https://") ||
			strings.HasSuffix(subCmd, ".js") || strings.HasSuffix(subCmd, ".json") ||
			strings.HasSuffix(subCmd, ".yaml") || strings.HasSuffix(subCmd, ".yml") ||
			strings.HasSuffix(subCmd, ".proto") || strings.HasSuffix(subCmd, ".har") {
			return c.runAdd(ctx, args, stdout, stderr)
		}

		return fmt.Errorf("unknown upstream action %q (supported: add, list, sync)", subCmd)
	}
}

func (c *CmdUpstream) runAdd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("upstream add", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		nameFlag = fs.String("name", "", "Custom target filename or identifier")
		typeFlag = fs.String("type", "", "Explicit asset category (js, schemas, proto, har)")
		pkgFlag  = fs.String("pkg", "", "Associate upstream source with a specific package/contract in .vortex.yml")
	)

	if err := fs.Parse(args); err != nil {
		return err
	}

	posArgs := fs.Args()
	if len(posArgs) == 0 {
		return errors.New("upstream add requires a source file path or URL (e.g. `vortex upstream add m=AgQvWc.js`)")
	}

	source := posArgs[0]

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	rootDir, _, _ := project.FindRoot(cwd)

	// Ensure .gitattributes and .gitignore are up to date
	_, _ = project.EnsureGitattributes(rootDir)
	_, _ = project.EnsureGitignore(rootDir)

	for _, src := range strings.Split(source, ",") {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}

		if err := c.ingestOne(ctx, rootDir, src, *nameFlag, *typeFlag, *pkgFlag, stdout); err != nil {
			return err
		}
	}

	return nil
}

func (c *CmdUpstream) ingestOne(
	ctx context.Context,
	rootDir, src, customName, explicitType, pkgName string,
	stdout io.Writer,
) error {
	var (
		data     []byte
		filename string
		category string
	)

	isURL := strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")

	if isURL {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return fmt.Errorf("creating HTTP request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("fetching upstream URL %s: %w", src, err)
		}

		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("HTTP error %d fetching upstream %s", resp.StatusCode, src)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}

		data = body
		filename = filepath.Base(resp.Request.URL.Path)

		if filename == "" || filename == "/" || filename == "." {
			filename = "upstream_spec.json"
		}
	} else {
		// Local file path
		fileData, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading local file %s: %w", src, err)
		}

		data = fileData
		filename = filepath.Base(src)
	}

	if customName != "" {
		filename = customName
	}

	// Detect category
	category = explicitType
	if category == "" {
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".js", ".mjs", ".ts":
			category = "js"
		case ".json", ".yaml", ".yml":
			category = "schemas"
		case ".proto":
			category = "proto"
		case ".har":
			category = "har"
		default:
			category = "assets"
		}
	}

	targetDir := filepath.Join(rootDir, ".vortex", "upstream", category)
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", targetDir, err)
	}

	targetPath := filepath.Join(targetDir, filename)

	// #nosec G306
	if err := os.WriteFile(targetPath, data, 0o600); err != nil {
		return fmt.Errorf("writing upstream file %s: %w", targetPath, err)
	}

	// Compute SHA256
	h := sha256.Sum256(data)
	hashStr := hex.EncodeToString(h[:8])

	relPath, _ := filepath.Rel(rootDir, targetPath)
	relSlash := filepath.ToSlash(relPath)

	fmt.Fprintf(stdout, "✔ Ingested upstream asset: %s\n", relSlash)
	fmt.Fprintf(stdout, "  • Category:    %s\n", category)
	fmt.Fprintf(stdout, "  • Size:        %s (%d bytes)\n", formatBytes(int64(len(data))), len(data))
	fmt.Fprintf(stdout, "  • SHA256:      %s...\n", hashStr)
	fmt.Fprintf(stdout, "  • Review Diff: Collapsed by default (.gitattributes)\n")

	// If source is not in .vortex and is a local file in current dir, offer or perform cleanup
	if !isURL && !strings.Contains(src, ".vortex") {
		cleanSrc := filepath.Clean(src)
		cleanTarget := filepath.Clean(targetPath)

		if cleanSrc != cleanTarget && filepath.Dir(cleanSrc) == "." {
			_ = os.Remove(cleanSrc)
			fmt.Fprintf(stdout, "  • Source:      Moved from %s to %s\n", cleanSrc, relSlash)
		}
	}

	// Register in .vortex.yml if package is specified or found
	if pkgName != "" {
		cfg, _ := project.Load(rootDir)
		if cfg != nil {
			ct := cfg.FindContract(pkgName)
			if ct != nil {
				ct.Upstream = &project.UpstreamConfig{
					Source: relSlash,
					Format: category,
				}
				_ = cfg.Save()

				fmt.Fprintf(stdout, "  • Contract:    Associated with %s in .vortex.yml\n", ct.Name)
			}
		}
	}

	fmt.Fprintln(stdout)

	return nil
}

func (c *CmdUpstream) runList(stdout, stderr io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	rootDir, _, _ := project.FindRoot(cwd)
	upstreamRoot := filepath.Join(rootDir, ".vortex", "upstream")

	if _, err := os.Stat(upstreamRoot); os.IsNotExist(err) {
		fmt.Fprintf(stdout, "No upstream assets tracked yet in .vortex/upstream/.\n")
		fmt.Fprintf(stdout, "Use `vortex upstream add <file|url>` to ingest upstream JS bundles or schemas.\n")

		return nil
	}

	fmt.Fprintf(stdout, "Tracked Upstream Reverse-Engineering Assets (.vortex/upstream/):\n\n")
	fmt.Fprintf(stdout, "  %-10s %-32s %-12s %-10s %s\n", "CATEGORY", "FILENAME", "SIZE", "HASH", "MODIFIED")
	fmt.Fprintf(stdout, "  %s\n", strings.Repeat("─", 80))

	count := 0

	_ = filepath.Walk(upstreamRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(upstreamRoot, path)
		parts := strings.Split(filepath.ToSlash(relPath), "/")
		category := "assets"
		filename := info.Name()

		if len(parts) > 1 {
			category = parts[0]
			filename = filepath.ToSlash(filepath.Join(parts[1:]...))
		}

		//nolint:gosec
		data, rErr := os.ReadFile(path)
		hashStr := "--------"

		if rErr == nil {
			h := sha256.Sum256(data)
			hashStr = hex.EncodeToString(h[:4])
		}

		modTime := info.ModTime().Format("2006-01-02 15:04")
		fmt.Fprintf(
			stdout,
			"  %-10s %-32s %-12s %-10s %s\n",
			category,
			filename,
			formatBytes(info.Size()),
			hashStr,
			modTime,
		)

		count++

		return nil
	})

	fmt.Fprintf(stdout, "\nTotal: %d tracked upstream asset(s)\n", count)

	return nil
}

func (c *CmdUpstream) runSync(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	rootDir, _, _ := project.FindRoot(cwd)

	cfg, err := project.Load(rootDir)
	if err != nil || cfg == nil {
		return errors.New("no .vortex.yml configuration found to sync")
	}

	synced := 0

	for _, ct := range cfg.Contracts {
		if ct.Upstream != nil && strings.HasPrefix(ct.Upstream.Source, "http") {
			fmt.Fprintf(stdout, "Syncing %s from %s...\n", ct.Name, ct.Upstream.Source)

			if err := c.ingestOne(
				ctx,
				rootDir,
				ct.Upstream.Source,
				"",
				ct.Upstream.Format,
				ct.Name,
				stdout,
			); err != nil {
				fmt.Fprintf(stderr, "⚠️ Failed syncing %s: %v\n", ct.Name, err)
			} else {
				synced++
			}
		}
	}

	if synced == 0 {
		fmt.Fprintf(stdout, "No remote HTTP/HTTPS upstream sources configured in .vortex.yml.\n")
	}

	return nil
}
