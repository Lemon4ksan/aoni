// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdTag manages API contract release versions and SemVer snapshots.
type CmdTag struct{}

func (c *CmdTag) Name() string      { return "tag" }
func (c *CmdTag) Aliases() []string { return []string{"release", "semver"} }
func (c *CmdTag) Synopsis() string {
	return "Manage API contract release tags, SemVer snapshots, and changelogs"
}

func (c *CmdTag) Usage() string {
	return "vortex tag [list|add|rm|show] [version] [-m message] [contract] [flags]"
}

// TagEntry records a snapshot of a contract at a specific SemVer release.
type TagEntry struct {
	Version   string    `json:"version"`
	Contract  string    `json:"contract"`
	File      string    `json:"file"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
	Methods   []string  `json:"methods,omitempty"`
}

// TagDatabase holds workspace release snapshots in .vortex/tags.json.
type TagDatabase struct {
	Tags []TagEntry `json:"tags"`
}

func (c *CmdTag) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("tag", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dirFlag     = fs.String("dir", "", "Target workspace directory (default: current root)")
		msgFlag     = fs.String("m", "", "Release message or changelog summary")
		gitFlag     = fs.Bool("git", true, "Also create/manage git lightweight tag")
		jsonFlag    = fs.Bool("json", false, "Output in JSON format")
		gitOnlyFlag = fs.Bool("from-git", false, "Read tags exclusively from Git history")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex tag — API Contract SemVer & Release Snapshot Manager\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex tag [list] [--json]                            List all contract release tags\n")
		fmt.Fprintf(stderr, "  vortex tag add <version> [-m \"message\"] [contract]    Record a release tag snapshot\n")
		fmt.Fprintf(stderr, "  vortex tag rm <version> [contract]                    Delete a release tag\n")
		fmt.Fprintf(
			stderr,
			"  vortex tag show <version>                             Inspect details of a release tag\n\n",
		)
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex tag add v1.2.0 -m \"Release with inventory filters\"\n")
		fmt.Fprintf(stderr, "  vortex tag add v1.3.0 Market -m \"Add instant buy order endpoints\"\n")
		fmt.Fprintf(stderr, "  vortex tag list\n")
		fmt.Fprintf(stderr, "  vortex tag show v1.2.0\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-dir" || arg == "-m") &&
				i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			nonFlags = append(nonFlags, arg)
		}
	}

	if err := fs.Parse(append(flags, nonFlags...)); err != nil {
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

	cfg, _ := project.Load(targetDir)

	action := "list"

	var versionArg, contractArg string

	posArgs := fs.Args()
	if len(posArgs) > 0 {
		first := strings.ToLower(posArgs[0])
		switch first {
		case "list", "ls":
			action = "list"

			if len(posArgs) > 1 {
				contractArg = posArgs[1]
			}

		case "add", "create", "set":
			action = "add"

			if len(posArgs) > 1 {
				versionArg = posArgs[1]
			}

			if len(posArgs) > 2 {
				contractArg = posArgs[2]
			}

		case "rm", "delete", "del":
			action = "rm"

			if len(posArgs) > 1 {
				versionArg = posArgs[1]
			}

			if len(posArgs) > 2 {
				contractArg = posArgs[2]
			}

		case "show", "inspect", "info":
			action = "show"

			if len(posArgs) > 1 {
				versionArg = posArgs[1]
			}

		default:
			// If looks like a version "v1.0.0", treat as add or show
			if strings.HasPrefix(first, "v") && strings.Contains(first, ".") {
				versionArg = posArgs[0]
				if *msgFlag != "" || len(posArgs) > 1 {
					action = "add"

					if len(posArgs) > 1 {
						contractArg = posArgs[1]
					}
				} else {
					action = "show"
				}
			} else {
				action = "list"
				contractArg = posArgs[0]
			}
		}
	}

	db, _ := loadTagDatabase(targetDir)

	switch action {
	case "list":
		return c.runList(ctx, stdout, targetDir, db, contractArg, *jsonFlag, *gitOnlyFlag)
	case "add":
		return c.runAdd(ctx, stdout, targetDir, cfg, db, versionArg, contractArg, *msgFlag, *gitFlag)
	case "rm":
		return c.runRm(ctx, stdout, targetDir, db, versionArg, contractArg, *gitFlag)
	case "show":
		return c.runShow(ctx, stdout, targetDir, cfg, db, versionArg, *jsonFlag)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func (c *CmdTag) runList(
	ctx context.Context,
	stdout io.Writer,
	rootDir string,
	db *TagDatabase,
	filterContract string,
	jsonOut, fromGit bool,
) error {
	var tags []TagEntry

	if !fromGit {
		for _, t := range db.Tags {
			if filterContract == "" || strings.EqualFold(t.Contract, filterContract) {
				tags = append(tags, t)
			}
		}
	}

	// Also discover Git tags matching v*.*
	// #nosec G204,G702
	cmd := exec.CommandContext(
		ctx,
		"git",
		"tag",
		"-l",
		"--sort=-creatordate",
		"--format=%(refname:short)|%(creatordate:iso)|%(subject)",
	)
	cmd.Dir = rootDir

	out, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			parts := strings.SplitN(line, "|", 3)
			if len(parts) >= 1 && strings.HasPrefix(parts[0], "v") {
				vName := parts[0]
				// Check if already in db
				alreadyInDB := false
				for _, t := range tags {
					if t.Version == vName {
						alreadyInDB = true
						break
					}
				}

				if !alreadyInDB {
					date := time.Now()
					if len(parts) >= 2 {
						if t, pErr := time.Parse("2006-01-02 15:04:05 -0700", parts[1]); pErr == nil {
							date = t
						}
					}

					msg := "-"
					if len(parts) >= 3 && parts[2] != "" {
						msg = parts[2]
					}

					tags = append(tags, TagEntry{
						Version:   vName,
						Contract:  "(workspace)",
						Message:   msg,
						CreatedAt: date,
					})
				}
			}
		}
	}

	sort.Slice(tags, func(i, j int) bool {
		return tags[i].CreatedAt.After(tags[j].CreatedAt)
	})

	if jsonOut {
		data, jErr := json.MarshalIndent(tags, "", "  ")
		if jErr != nil {
			return jErr
		}

		fmt.Fprintln(stdout, string(data))

		return nil
	}

	fmt.Fprintf(stdout, "⚡ Vortex API Contract Releases (%s)\n\n", rootDir)

	if len(tags) == 0 {
		fmt.Fprintf(stdout, "No API release tags found. Create one with `vortex tag add v1.0.0 -m \"Message\"`.\n")
		return nil
	}

	fmt.Fprintf(stdout, "  %-12s %-12s %-20s %s\n", "TAG", "DATE", "CONTRACT / SCOPE", "MESSAGE")
	fmt.Fprintf(stdout, "  %s\n", strings.Repeat("─", 80))

	for _, t := range tags {
		dateStr := t.CreatedAt.Format("2006-01-02")
		fmt.Fprintf(stdout, "  %-12s %-12s %-20s %s\n", t.Version, dateStr, truncateString(t.Contract, 18), t.Message)
	}

	return nil
}

func (c *CmdTag) runAdd(
	ctx context.Context,
	stdout io.Writer,
	rootDir string,
	cfg *project.Config,
	db *TagDatabase,
	version, targetContract, message string,
	createGitTag bool,
) error {
	if version == "" {
		return errors.New("usage: vortex tag add <version> [-m \"message\"] [contract]")
	}

	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	contractScope := "(workspace)"
	contractFile := ""

	var methods []string

	p := parser.NewParser()

	if targetContract != "" && cfg != nil {
		for _, ct := range cfg.Contracts {
			if strings.EqualFold(ct.Name, targetContract) || strings.EqualFold(ct.Package, targetContract) ||
				strings.EqualFold(ct.File, targetContract) {
				contractScope = ct.Name

				contractFile = ct.File
				if root, err := p.ParseFile(filepath.Join(rootDir, ct.File)); err == nil {
					for _, svc := range root.Services {
						for _, m := range svc.Methods {
							methods = append(methods, m.Name)
						}
					}
				}

				break
			}
		}
	} else if cfg != nil && len(cfg.Contracts) > 0 {
		for _, ct := range cfg.Contracts {
			if root, err := p.ParseFile(filepath.Join(rootDir, ct.File)); err == nil {
				for _, svc := range root.Services {
					for _, m := range svc.Methods {
						methods = append(methods, svc.Name+"."+m.Name)
					}
				}
			}
		}
	}

	if message == "" {
		message = "API release " + version
	}

	entry := TagEntry{
		Version:   version,
		Contract:  contractScope,
		File:      contractFile,
		Message:   message,
		CreatedAt: time.Now(),
		Methods:   methods,
	}

	// Update DB (replace if exists)
	replaced := false
	for i, t := range db.Tags {
		if t.Version == version && t.Contract == contractScope {
			db.Tags[i] = entry
			replaced = true
			break
		}
	}

	if !replaced {
		db.Tags = append(db.Tags, entry)
	}

	_ = saveTagDatabase(rootDir, db)

	// Optional Git tag
	if createGitTag {
		gitTagName := version
		if contractScope != "(workspace)" {
			gitTagName = fmt.Sprintf("api/%s/%s", strings.ToLower(contractScope), version)
		}

		// #nosec G204,G702
		cmd := exec.CommandContext(ctx, "git", "tag", "-a", gitTagName, "-m", message)
		cmd.Dir = rootDir
		_ = cmd.Run()
	}

	// Update @version directive in target contract file if present
	if contractFile != "" {
		filePath := filepath.Join(rootDir, contractFile)
		if src, rErr := os.ReadFile(filePath); rErr == nil {
			lines := strings.Split(string(src), "\n")

			hasVersion := false
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "// @version") {
					lines[i] = fmt.Sprintf("// @version %q", version)
					hasVersion = true
					break
				}
			}

			if !hasVersion {
				for i, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "// @aoni:service") {
						lines = append(
							lines[:i+1],
							append([]string{fmt.Sprintf("// @version %q", version)}, lines[i+1:]...)...)
						hasVersion = true

						break
					}
				}
			}

			if hasVersion {
				_ = os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0o600)
			}
		}
	}

	fmt.Fprintf(stdout, "✔ Created API release tag %s [%s]\n", version, contractScope)
	fmt.Fprintf(stdout, "  • Message:  %s\n", message)
	fmt.Fprintf(stdout, "  • Methods:  %d endpoints snapshotted\n", len(methods))

	return nil
}

func (c *CmdTag) runRm(
	ctx context.Context,
	stdout io.Writer,
	rootDir string,
	db *TagDatabase,
	version, targetContract string,
	deleteGitTag bool,
) error {
	if version == "" {
		return errors.New("usage: vortex tag rm <version> [contract]")
	}

	var filtered []TagEntry

	removed := false
	for _, t := range db.Tags {
		if t.Version == version && (targetContract == "" || strings.EqualFold(t.Contract, targetContract)) {
			removed = true
			continue
		}

		filtered = append(filtered, t)
	}

	db.Tags = filtered
	_ = saveTagDatabase(rootDir, db)

	if deleteGitTag {
		// #nosec G204,G702
		cmd := exec.CommandContext(ctx, "git", "tag", "-d", version)
		cmd.Dir = rootDir
		_ = cmd.Run()
	}

	if !removed && !deleteGitTag {
		return fmt.Errorf("tag %q not found", version)
	}

	fmt.Fprintf(stdout, "✔ Removed API release tag %s\n", version)

	return nil
}

func (c *CmdTag) runShow(
	ctx context.Context,
	stdout io.Writer,
	rootDir string,
	cfg *project.Config,
	db *TagDatabase,
	version string,
	jsonOut bool,
) error {
	if version == "" {
		return errors.New("usage: vortex tag show <version>")
	}

	var found *TagEntry
	for _, t := range db.Tags {
		if strings.EqualFold(t.Version, version) {
			found = &t
			break
		}
	}

	// Fallback to reading Git ref directly in-memory
	if found == nil {
		// Query Git commit log for this tag
		// #nosec G204,G702
		cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%ai|%s", version)
		cmd.Dir = rootDir

		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("release tag %q not found in Git history or local snapshots", version)
		}

		parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)

		date := time.Now()
		if len(parts) >= 1 {
			if t, pErr := time.Parse("2006-01-02 15:04:05 -0700", parts[0]); pErr == nil {
				date = t
			}
		}

		msg := "Git release tag"
		if len(parts) >= 2 {
			msg = parts[1]
		}

		// Reconstruct methods from contracts in git at that ref
		var methods []string

		p := parser.NewParser()
		if cfg != nil && len(cfg.Contracts) > 0 {
			for _, ct := range cfg.Contracts {
				// Read file bytes from Git at the tag ref
				// #nosec G204,G702
				showCmd := exec.CommandContext(
					ctx,
					"git",
					"show",
					fmt.Sprintf("%s:%s", version, filepath.ToSlash(ct.File)),
				)

				showCmd.Dir = rootDir
				if fileBytes, showErr := showCmd.Output(); showErr == nil {
					if root, parseErr := p.ParseSource(ct.File, fileBytes); parseErr == nil {
						for _, svc := range root.Services {
							for _, m := range svc.Methods {
								methods = append(methods, svc.Name+"."+m.Name)
							}
						}
					}
				}
			}
		}

		found = &TagEntry{
			Version:   version,
			Contract:  "(workspace)",
			Message:   msg,
			CreatedAt: date,
			Methods:   methods,
		}
	}

	if jsonOut {
		data, _ := json.MarshalIndent(found, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	fmt.Fprintf(stdout, "⚡ API Release Snapshot: %s\n\n", found.Version)
	fmt.Fprintf(stdout, "  • Contract:   %s\n", found.Contract)

	if found.File != "" {
		fmt.Fprintf(stdout, "  • File:       %s\n", found.File)
	}

	fmt.Fprintf(stdout, "  • Date:       %s\n", found.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(stdout, "  • Message:    %s\n", found.Message)
	fmt.Fprintf(stdout, "  • Endpoints:  %d methods\n\n", len(found.Methods))

	for _, m := range found.Methods {
		fmt.Fprintf(stdout, "    ↳ %s\n", m)
	}

	return nil
}

func loadTagDatabase(rootDir string) (*TagDatabase, error) {
	tagFile := filepath.Join(rootDir, ".vortex", "tags.json")

	data, err := os.ReadFile(tagFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &TagDatabase{}, nil
		}

		return nil, fmt.Errorf("reading tags database: %w", err)
	}

	var db TagDatabase
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("parsing tags database: %w", err)
	}

	return &db, nil
}

func saveTagDatabase(rootDir string, db *TagDatabase) error {
	dir := filepath.Join(rootDir, ".vortex")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "tags.json"), data, 0o600)
}
