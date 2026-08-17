// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/cache"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdCache implements the `vortex cache` command suite for managing traffic captures and secret vaults.
type CmdCache struct{}

func (c *CmdCache) Name() string      { return "cache" }
func (c *CmdCache) Aliases() []string { return []string{"traffic"} }
func (c *CmdCache) Synopsis() string {
	return "Manage local traffic captures and secret credentials vault"
}

func (c *CmdCache) Usage() string {
	return "vortex cache [list|show|store|sanitize|export|secrets|prune] [flags]"
}

func (c *CmdCache) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	args = NormalizeArgs(args)

	if len(args) == 0 {
		return c.printHelp(stderr)
	}

	sub := strings.ToLower(args[0])
	subArgs := args[1:]

	switch sub {
	case "list", "ls":
		return c.runList(ctx, subArgs, stdout, stderr)
	case "show", "inspect":
		return c.runShow(ctx, subArgs, stdout, stderr)
	case "store", "add", "put":
		return c.runStore(ctx, subArgs, stdout, stderr)
	case "sanitize", "clean-har":
		return c.runSanitize(ctx, subArgs, stdout, stderr)
	case "export", "restore":
		return c.runExport(ctx, subArgs, stdout, stderr)
	case "secrets", "vault", "secret":
		return c.runSecrets(ctx, subArgs, stdout, stderr)
	case "prune", "clean":
		return c.runPrune(ctx, subArgs, stdout, stderr)
	case "help", "-h", "--help":
		return c.printHelp(stderr)
	default:
		// Default to store if args look like files
		if strings.HasSuffix(sub, ".har") || strings.HasSuffix(sub, ".json") {
			return c.runStore(ctx, args, stdout, stderr)
		}

		return fmt.Errorf("unknown cache subcommand %q (run 'vortex cache help' for usage)", sub)
	}
}

func (c *CmdCache) printHelp(stderr io.Writer) error {
	fmt.Fprintf(stderr, "vortex cache — Manage Local Traffic Captures & Secrets Vault\n\n")
	fmt.Fprintf(stderr, "Usage:\n")
	fmt.Fprintf(stderr, "  vortex cache list                          List cached traffic sessions\n")
	fmt.Fprintf(stderr, "  vortex cache show <id|hash>                Show metadata of a cached session\n")
	fmt.Fprintf(stderr, "  vortex cache store [--move] <files...>     Archive HAR files into cache\n")
	fmt.Fprintf(stderr, "  vortex cache sanitize <file> -out=<clean>  Export scrubbed, Git-safe HAR\n")
	fmt.Fprintf(stderr, "  vortex cache export <id|hash> -out=<file>  Restore uncompressed HAR from cache\n")
	fmt.Fprintf(stderr, "  vortex cache secrets [list|get|set|clear]  Manage local credentials vault\n")
	fmt.Fprintf(stderr, "  vortex cache prune [--all]                 Clean up expired/unused traffic\n\n")

	return nil
}

func (c *CmdCache) getRootDir() string {
	cwd, _ := os.Getwd()
	if root, _, err := project.FindRoot(cwd); err == nil && root != "" {
		return root
	}

	return cwd
}

func (c *CmdCache) runList(_ context.Context, _ []string, stdout, stderr io.Writer) error {
	rootDir := c.getRootDir()

	list, err := cache.ListTraffic(rootDir)
	if err != nil {
		return fmt.Errorf("listing cache: %w", err)
	}

	if len(list) == 0 {
		fmt.Fprintf(
			stdout,
			"No traffic captures stored in .vortex/cache/traffic (use 'vortex cache store <file.har>')\n",
		)

		return nil
	}

	fmt.Fprintf(stdout, "⚡ Vortex Traffic Cache (.vortex/cache/traffic)\n\n")
	fmt.Fprintf(stdout, "%-14s %-24s %-28s %-10s %-16s %-10s %s\n",
		"ID", "ORIGINAL FILE", "DOMAINS", "ENDPOINTS", "RAW -> GZ", "SANITIZED", "DATE")
	fmt.Fprintf(stdout, "%s\n", strings.Repeat("─", 115))

	for _, e := range list {
		originsStr := strings.Join(e.Origins, ", ")
		if len(originsStr) > 26 {
			originsStr = originsStr[:23] + "..."
		}

		origFile := e.OriginalFile
		if len(origFile) > 22 {
			origFile = origFile[:19] + "..."
		}

		sizeRatio := fmt.Sprintf("%s -> %s", formatBytes(e.SizeBytes), formatBytes(e.CompressedBytes))

		sanStr := "✔ yes"
		if !e.Sanitized {
			sanStr = "raw"
		}

		fmt.Fprintf(stdout, "%-14s %-24s %-28s %-10d %-16s %-10s %s\n",
			e.ID, origFile, originsStr, e.EndpointCount, sizeRatio, sanStr, e.StoredAt.Format("2006-01-02 15:04"))
	}

	fmt.Fprintf(stdout, "\nTotal: %d session(s)\n", len(list))

	return nil
}

func (c *CmdCache) runShow(_ context.Context, args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing session ID or hash prefix (e.g. 'vortex cache show aistudio')")
	}

	rootDir := c.getRootDir()

	data, entry, err := cache.GetTraffic(rootDir, args[0])
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "⚡ Traffic Session: %s\n", entry.ID)
	fmt.Fprintf(stdout, "  Hash:            %s\n", entry.Hash)
	fmt.Fprintf(stdout, "  Original File:   %s\n", entry.OriginalFile)
	fmt.Fprintf(stdout, "  Captured Hosts:  %s\n", strings.Join(entry.Origins, ", "))
	fmt.Fprintf(stdout, "  Endpoints:       %d\n", entry.EndpointCount)
	fmt.Fprintf(stdout, "  Uncompressed:    %s (%d bytes)\n", formatBytes(entry.SizeBytes), entry.SizeBytes)
	fmt.Fprintf(stdout, "  Compressed:      %s (%d bytes)\n", formatBytes(entry.CompressedBytes), entry.CompressedBytes)
	fmt.Fprintf(stdout, "  Sanitized:       %t\n", entry.Sanitized)
	fmt.Fprintf(stdout, "  Stored At:       %s\n", entry.StoredAt.Format(time.RFC3339))
	fmt.Fprintf(stdout, "  Decompressed:    %d bytes verified\n", len(data))

	return nil
}

func parseFlagsPreserveOrder(fs *flag.FlagSet, args []string) error {
	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-out" || arg == "-o" || arg == "-older-than") && i+1 < len(args) &&
				!strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			nonFlags = append(nonFlags, arg)
		}
	}

	return fs.Parse(append(flags, nonFlags...))
}

func (c *CmdCache) runStore(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cache store", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		moveFlag     = fs.Bool("move", false, "Move original file into cache (deletes source file on success)")
		sanitizeFlag = fs.Bool("sanitize", true, "Sanitize tokens and credentials before storing")
	)

	if err := parseFlagsPreserveOrder(fs, args); err != nil {
		return err
	}

	files := fs.Args()
	if len(files) == 0 {
		return errors.New("no HAR files specified to store (e.g. 'vortex cache store session.har')")
	}

	rootDir := c.getRootDir()
	vault, vaultPath, _ := cache.LoadSecrets(rootDir)

	storedCount := 0

	for _, rawPath := range files {
		for _, fPath := range strings.Split(rawPath, ",") {
			fPath = strings.TrimSpace(fPath)
			if fPath == "" {
				continue
			}

			data, err := os.ReadFile(fPath)
			if err != nil {
				fmt.Fprintf(stderr, "⚠️  Failed reading %s: %v\n", fPath, err)
				continue
			}

			entry, secrets, err := cache.StoreTraffic(rootDir, fPath, data, *moveFlag, *sanitizeFlag)
			if err != nil {
				fmt.Fprintf(stderr, "⚠️  Failed caching %s: %v\n", fPath, err)
				continue
			}

			// Store extracted secrets in vault
			for k, s := range secrets {
				vault.SetWithTarget(k, s.Value, entry.ID, s.Header, s.Query, s.Cookie)
			}

			action := "Stored"
			if *moveFlag {
				action = "Moved"
			}

			fmt.Fprintf(
				stdout,
				"✔ %s %s in cache as %s (%s -> %s, %d endpoints)\n",
				action,
				fPath,
				entry.ID,
				formatBytes(entry.SizeBytes),
				formatBytes(entry.CompressedBytes),
				entry.EndpointCount,
			)

			storedCount++
		}
	}

	if len(vault.Secrets) > 0 {
		_ = vault.Save(vaultPath)
		fmt.Fprintf(stdout, "✔ Updated secrets vault with %d credential(s)\n", len(vault.Secrets))
	}

	return nil
}

func (c *CmdCache) runSanitize(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cache sanitize", flag.ContinueOnError)
	fs.SetOutput(stderr)

	outFile := fs.String("out", "sanitized.har", "Output sanitized HAR file path")

	if err := parseFlagsPreserveOrder(fs, args); err != nil {
		return err
	}

	if len(fs.Args()) == 0 {
		return errors.New("missing input HAR file (e.g. 'vortex cache sanitize session.har -out=safe.har')")
	}

	srcFile := fs.Args()[0]

	data, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("reading source HAR: %w", err)
	}

	sanitized, secrets, err := cache.SanitizeHAR(data)
	if err != nil {
		return fmt.Errorf("sanitizing HAR: %w", err)
	}

	if err := os.WriteFile(*outFile, sanitized, 0o600); err != nil {
		return fmt.Errorf("writing sanitized HAR: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Exported Git-safe sanitized HAR to %s (%d bytes, %d secrets masked)\n",
		*outFile, len(sanitized), len(secrets))

	return nil
}

func (c *CmdCache) runExport(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cache export", flag.ContinueOnError)
	fs.SetOutput(stderr)

	outFile := fs.String("out", "restored.har", "Output uncompressed HAR file path")

	if err := parseFlagsPreserveOrder(fs, args); err != nil {
		return err
	}

	if len(fs.Args()) == 0 {
		return errors.New("missing session ID or hash prefix (e.g. 'vortex cache export aistudio -out=restored.har')")
	}

	rootDir := c.getRootDir()

	data, entry, err := cache.GetTraffic(rootDir, fs.Args()[0])
	if err != nil {
		return err
	}

	if err := os.WriteFile(*outFile, data, 0o600); err != nil {
		return fmt.Errorf("writing restored HAR: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Restored %s to %s (%s uncompressed)\n", entry.ID, *outFile, formatBytes(entry.SizeBytes))

	return nil
}

func (c *CmdCache) runSecrets(_ context.Context, args []string, stdout, stderr io.Writer) error {
	rootDir := c.getRootDir()

	vault, vaultPath, err := cache.LoadSecrets(rootDir)
	if err != nil {
		return fmt.Errorf("loading secrets vault: %w", err)
	}

	if len(args) == 0 || args[0] == "list" || args[0] == "ls" {
		secrets := vault.All()
		if len(secrets) == 0 {
			fmt.Fprintf(stdout, "No credentials in local vault .vortex/cache/secrets.json\n")
			return nil
		}

		fmt.Fprintf(stdout, "🔑 Vortex Local Credentials Vault (.vortex/cache/secrets.json)\n\n")
		fmt.Fprintf(stdout, "%-24s %-32s %-20s %s\n", "KEY", "MASKED VALUE", "ORIGIN", "UPDATED")
		fmt.Fprintf(stdout, "%s\n", strings.Repeat("─", 90))

		for _, s := range secrets {
			origin := s.Origin
			if origin == "" {
				origin = "manual"
			}

			fmt.Fprintf(stdout, "%-24s %-32s %-20s %s\n",
				s.Key, s.Masked, origin, s.UpdatedAt.Format("2006-01-02 15:04"))
		}

		fmt.Fprintf(stdout, "\nUse 'option.FromVortexCache()' in client to auto-inject credentials at runtime.\n")

		return nil
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "get":
		if len(args) < 2 {
			return errors.New("missing secret key name (e.g. 'vortex cache secrets get AUTH_TOKEN')")
		}

		val, ok := vault.Get(args[1])
		if !ok {
			return fmt.Errorf("secret %q not found in vault", args[1])
		}

		fmt.Fprintln(stdout, val)

		return nil

	case "set":
		if len(args) < 3 {
			return errors.New("usage: vortex cache secrets set <KEY> <VALUE>")
		}

		vault.Set(args[1], args[2], "manual")

		if err := vault.Save(vaultPath); err != nil {
			return fmt.Errorf("saving vault: %w", err)
		}

		fmt.Fprintf(stdout, "✔ Saved %s to local secrets vault\n", args[1])

		return nil

	case "clear", "purge":
		vault.Clear()

		if err := vault.Save(vaultPath); err != nil {
			return fmt.Errorf("saving vault: %w", err)
		}

		fmt.Fprintf(stdout, "✔ Cleared all credentials from local vault\n")

		return nil

	default:
		return fmt.Errorf("unknown secrets action %q (use list, get, set, clear)", sub)
	}
}

func (c *CmdCache) runPrune(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("cache prune", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		allFlag       = fs.Bool("all", false, "Remove all cached traffic sessions")
		olderThanFlag = fs.String("older-than", "", "Remove sessions older than duration (e.g. 720h, 30d)")
	)

	if err := fs.Parse(args); err != nil {
		return err
	}

	var dur time.Duration
	if *olderThanFlag != "" {
		s := *olderThanFlag
		if strings.HasSuffix(s, "d") {
			daysStr := strings.TrimSuffix(s, "d")

			var days int

			_, _ = fmt.Sscanf(daysStr, "%d", &days)
			dur = time.Duration(days) * 24 * time.Hour
		} else {
			var parseErr error

			dur, parseErr = time.ParseDuration(s)
			if parseErr != nil {
				return fmt.Errorf("invalid duration format %q: %w", s, parseErr)
			}
		}
	}

	rootDir := c.getRootDir()

	removed, err := cache.PruneTraffic(rootDir, dur, *allFlag)
	if err != nil {
		return fmt.Errorf("pruning cache: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Removed %d cached traffic session(s)\n", removed)

	return nil
}
