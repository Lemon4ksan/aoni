// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package workspace

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

// CmdClean implements the 'vortex clean' subcommand.
type CmdClean struct{}

func (c *CmdClean) Name() string { return "clean" }

func (c *CmdClean) Aliases() []string { return []string{"cl", "clr"} }

func (c *CmdClean) Synopsis() string {
	return "Remove generated mock servers, harnesses, profiles, and coverage artifacts"
}

func (c *CmdClean) Usage() string {
	return "vortex clean [-all] [-dry-run] [-dir=.]"
}

func (c *CmdClean) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		allFlag    bool
		allA       bool
		dryRunFlag bool
		dryRunN    bool
		dirFlag    string
	)

	base.BoolVar(fs, &allFlag, "all", "", false, "Also remove primary generated API clients (*.gen.go)")
	base.BoolVar(fs, &allA, "a", "", false, "Alias for --all")
	base.BoolVar(fs, &dryRunFlag, "dry-run", "", false, "Preview files to be deleted without removing them")
	base.BoolVar(fs, &dryRunN, "n", "", false, "Alias for --dry-run")
	base.StringVar(fs, &dirFlag, "dir", "", "", "Target workspace directory (default: current repository root)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex clean — Clean workspace test artifacts, mocks, and caches\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex clean [-all] [-dry-run] [-dir=.]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	targetDir := dirFlag
	if targetDir == "" && len(posArgs) > 0 {
		targetDir = posArgs[0]
	}

	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	all := allFlag || allA
	dryRun := dryRunFlag || dryRunN

	type targetItem struct {
		path  string
		isDir bool
		size  int64
	}

	var toDelete []targetItem

	_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return filepath.SkipDir
		}

		name := d.Name()

		if d.IsDir() {
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".system_generated" {
				return filepath.SkipDir
			}

			if name == ".vortex" {
				var dirSize int64

				_ = filepath.WalkDir(path, func(_ string, subD os.DirEntry, _ error) error {
					if subD != nil && !subD.IsDir() {
						if info, err := subD.Info(); err == nil {
							dirSize += info.Size()
						}
					}

					return nil
				})

				toDelete = append(toDelete, targetItem{path: path, isDir: true, size: dirSize})

				return filepath.SkipDir
			}

			return nil
		}

		shouldDelete := false

		switch {
		case strings.HasSuffix(name, "_mock.gen.go"),
			strings.HasSuffix(name, "_harness.gen.go"),
			strings.HasSuffix(name, "_harness_test.go"),
			strings.HasSuffix(name, ".prof"),
			strings.HasSuffix(name, ".test"),
			strings.HasSuffix(name, ".test.exe"),
			strings.HasSuffix(name, ".cov"),
			strings.HasSuffix(name, ".sarif"),
			strings.HasSuffix(name, ".vortex.bak"),
			name == "cpu.prof",
			name == "mem.prof",
			name == "trace.out",
			name == "coverage.out",
			name == "coverage.html",
			name == "cover.out":
			shouldDelete = true

		case all && strings.HasSuffix(name, ".gen.go"):
			shouldDelete = true
		}

		if shouldDelete {
			var sz int64
			if info, err := d.Info(); err == nil {
				sz = info.Size()
			}

			toDelete = append(toDelete, targetItem{path: path, isDir: false, size: sz})
		}

		return nil
	})

	if len(toDelete) == 0 {
		fmt.Fprintf(stdout, "✨ Workspace is completely clean! No artifacts found to remove in %s\n", targetDir)
		return nil
	}

	var totalBytes int64
	for _, item := range toDelete {
		totalBytes += item.size
	}

	if dryRun {
		fmt.Fprintf(stdout, "🔍 Dry-run: Found %d artifact(s) to remove in %s (~%s):\n",
			len(toDelete), targetDir, formatBytes(totalBytes))

		for _, item := range toDelete {
			rel, _ := filepath.Rel(targetDir, item.path)
			if rel == "" {
				rel = item.path
			}

			if item.isDir {
				fmt.Fprintf(stdout, "  • [DIR]  %s/ (~%s)\n", filepath.ToSlash(rel), formatBytes(item.size))
			} else {
				fmt.Fprintf(stdout, "  • [FILE] %s (%s)\n", filepath.ToSlash(rel), formatBytes(item.size))
			}
		}

		fmt.Fprintf(stdout, "\nRun without --dry-run to delete these files.\n")

		return nil
	}

	deletedCount := 0
	for _, item := range toDelete {
		if item.isDir {
			if err := os.RemoveAll(item.path); err == nil {
				deletedCount++
			}
		} else {
			if err := os.Remove(item.path); err == nil {
				deletedCount++
			}
		}
	}

	fmt.Fprintf(stdout, "✔ Successfully removed %d workspace artifact(s) (freed %s)\n",
		deletedCount, formatBytes(totalBytes))

	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
