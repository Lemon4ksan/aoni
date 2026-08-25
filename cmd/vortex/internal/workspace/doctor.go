// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/tui"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

// CmdDoctor performs end-to-end diagnostic inspection of the Vortex workspace.
type CmdDoctor struct{}

func (c *CmdDoctor) Name() string      { return "doctor" }
func (c *CmdDoctor) Aliases() []string { return []string{"doc", "diag", "diagnose"} }
func (c *CmdDoctor) Synopsis() string {
	return "Diagnose workspace configuration, toolchain health, contract paths, and git synchronization"
}
func (c *CmdDoctor) Usage() string { return "vortex doctor [--fix] [--json]" }

// DoctorCheck represents a single diagnostic assertion.
type DoctorCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Warning bool   `json:"warning,omitempty"`
	Message string `json:"message"`
	FixHint string `json:"fix_hint,omitempty"`
}

// DoctorReport contains all diagnostic checks and recommendations.
type DoctorReport struct {
	WorkspaceRoot string        `json:"workspace_root"`
	GoVersion     string        `json:"go_version"`
	Platform      string        `json:"platform"`
	Checks        []DoctorCheck `json:"checks"`
	PassedCount   int           `json:"passed_count"`
	WarnCount     int           `json:"warn_count"`
	ErrorCount    int           `json:"error_count"`
	FixedCount    int           `json:"fixed_count,omitempty"`
}

func (c *CmdDoctor) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		fixFlag  bool
		jsonFlag bool
		dirFlag  string
	)

	base.BoolVar(fs, &fixFlag, "fix", "", false, "Automatically resolve fixable issues (gitignore, gitattributes)")
	base.BoolVar(fs, &jsonFlag, "json", "", false, "Output diagnostic report as JSON")
	base.StringVar(fs, &dirFlag, "dir", "", "", "Target workspace directory (default: current directory)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex doctor — Workspace Diagnostic & Health Inspector\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex doctor [flags]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  vortex doctor                                   # Audit workspace & toolchain health\n")
		fmt.Fprintf(stderr, "  vortex doctor --fix                             # Auto-heal git rules\n")
		fmt.Fprintf(stderr, "  vortex doctor --json                            # Machine-readable JSON telemetry\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	targetDir := dirFlag
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		targetDir = cwd
	}

	rootDir, configPath, _ := project.FindRoot(targetDir)

	rep := &DoctorReport{
		WorkspaceRoot: rootDir,
		GoVersion:     runtime.Version(),
		Platform:      fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Checks:        make([]DoctorCheck, 0, 10),
	}

	// 1. Check Go Toolchain
	rep.addCheck(DoctorCheck{
		Name:    "Go Toolchain",
		Passed:  true,
		Message: fmt.Sprintf("%s (%s/%s)", runtime.Version(), runtime.GOOS, runtime.GOARCH),
	})

	// 2. Check .vortex.yml Configuration
	cfg, loadErr := project.Load(rootDir)
	switch {
	case loadErr != nil:
		rep.addCheck(DoctorCheck{
			Name:    "Workspace Config (.vortex.yml)",
			Passed:  false,
			Message: fmt.Sprintf("Configuration error: %v", loadErr),
			FixHint: "Run `vortex init --force` to scaffold a valid configuration",
		})

	case configPath == "":
		rep.addCheck(DoctorCheck{
			Name:    "Workspace Config (.vortex.yml)",
			Passed:  true,
			Warning: true,
			Message: "Running in Zero-Config Auto-Discovery mode (no .vortex.yml found)",
			FixHint: "Run `vortex init` to persist explicit configuration",
		})

	default:
		rep.addCheck(DoctorCheck{
			Name:    "Workspace Config (.vortex.yml)",
			Passed:  true,
			Message: fmt.Sprintf("Valid configuration with %d declared contract(s)", len(cfg.Contracts)),
		})
	}

	// 3. Check Contract Paths Integrity
	if cfg != nil && len(cfg.Contracts) > 0 {
		missingCount := 0

		for _, ct := range cfg.Contracts {
			if ct.File == "" {
				missingCount++
				continue
			}

			targetFile := ct.File
			if !filepath.IsAbs(targetFile) {
				targetFile = filepath.Join(rootDir, targetFile)
			}

			fi, sErr := os.Stat(targetFile)
			if sErr != nil || fi.IsDir() {
				missingCount++
			}
		}

		if missingCount > 0 {
			rep.addCheck(DoctorCheck{
				Name:    "Contract Files on Disk",
				Passed:  false,
				Message: fmt.Sprintf("%d contract file(s) missing or point to directories", missingCount),
				FixHint: "Check file paths in .vortex.yml or run `vortex init <name>` to scaffold contracts",
			})
		} else {
			rep.addCheck(DoctorCheck{
				Name:    "Contract Files on Disk",
				Passed:  true,
				Message: fmt.Sprintf("All %d contract source file(s) exist on disk", len(cfg.Contracts)),
			})
		}
	}

	// 4. Check Generated Code Freshness
	if cfg != nil && len(cfg.Contracts) > 0 {
		statusEngine := project.NewStatusEngine()
		statusReport := statusEngine.Inspect(cfg, cfg.Contracts)

		if statusReport.StaleCount() > 0 {
			rep.addCheck(DoctorCheck{
				Name:    "Generated Code Freshness",
				Passed:  true,
				Warning: true,
				Message: fmt.Sprintf("%d generated client(s) out of date", statusReport.StaleCount()),
				FixHint: "Run `vortex gen` or `vortex` to rebuild stale code",
			})
		} else {
			rep.addCheck(DoctorCheck{
				Name:    "Generated Code Freshness",
				Passed:  true,
				Message: "All generated Go artifacts are 100% synchronized",
			})
		}
	}

	// 5. Check Git Invariants (.gitignore / .gitattributes)
	giPath := filepath.Join(rootDir, ".gitignore")
	giBytes, _ := os.ReadFile(giPath)
	giContent := string(giBytes)

	hasGitignoreRules := strings.Contains(giContent, "*_mock.gen.go") && strings.Contains(giContent, ".vortex/")
	if !hasGitignoreRules {
		if fixFlag {
			if updated, _ := project.EnsureGitignore(rootDir); updated {
				rep.FixedCount++
				rep.addCheck(DoctorCheck{
					Name:    ".gitignore Rules",
					Passed:  true,
					Message: "Auto-healed: added *.gen.go test mocks and .vortex/ cache patterns",
				})
			}
		} else {
			rep.addCheck(DoctorCheck{
				Name:    ".gitignore Rules",
				Passed:  true,
				Warning: true,
				Message: "Missing ignore rules for ephemeral mocks (*_mock.gen.go) or .vortex/",
				FixHint: "Run `vortex doctor --fix` to update .gitignore automatically",
			})
		}
	} else {
		rep.addCheck(DoctorCheck{
			Name:    ".gitignore Rules",
			Passed:  true,
			Message: "Clean: ephemeral mocks and cache directories are properly ignored",
		})
	}

	gaPath := filepath.Join(rootDir, ".gitattributes")
	gaBytes, _ := os.ReadFile(gaPath)
	gaContent := string(gaBytes)

	hasGitattributesRules := strings.Contains(gaContent, "linguist-generated=true")
	if !hasGitattributesRules {
		if fixFlag {
			if updated, _ := project.EnsureGitattributes(rootDir); updated {
				rep.FixedCount++
				rep.addCheck(DoctorCheck{
					Name:    ".gitattributes Annotations",
					Passed:  true,
					Message: "Auto-healed: added linguist-generated=true for *.gen.go files",
				})
			}
		} else {
			rep.addCheck(DoctorCheck{
				Name:    ".gitattributes Annotations",
				Passed:  true,
				Warning: true,
				Message: "Missing linguist-generated=true marker for generated files",
				FixHint: "Run `vortex doctor --fix` to update .gitattributes automatically",
			})
		}
	} else {
		rep.addCheck(DoctorCheck{
			Name:    ".gitattributes Annotations",
			Passed:  true,
			Message: "Clean: generated code is flagged with linguist-generated=true",
		})
	}

	// 6. Check Upstream Schema Reachability (if any remote URLs)
	if cfg != nil {
		client := &http.Client{Timeout: 3 * time.Second}

		for _, ct := range cfg.Contracts {
			if ct.Upstream != nil && ct.Upstream.Source != "" &&
				(strings.HasPrefix(ct.Upstream.Source, "http://") || strings.HasPrefix(ct.Upstream.Source, "https://")) {
				req, reqErr := http.NewRequestWithContext(ctx, http.MethodHead, ct.Upstream.Source, nil)
				if reqErr != nil {
					continue
				}

				resp, err := client.Do(req)
				if err != nil || (resp != nil && resp.StatusCode >= 400) {
					rep.addCheck(DoctorCheck{
						Name:    fmt.Sprintf("Upstream URL (%s)", ct.Name),
						Passed:  true,
						Warning: true,
						Message: fmt.Sprintf("Cannot reach %s (offline cache will be used)", ct.Upstream.Source),
						FixHint: "Verify network connectivity or check upstream URL in .vortex.yml",
					})
				} else {
					if resp != nil {
						_ = resp.Body.Close()
					}

					rep.addCheck(DoctorCheck{
						Name:    fmt.Sprintf("Upstream URL (%s)", ct.Name),
						Passed:  true,
						Message: "Reachable: " + ct.Upstream.Source,
					})
				}
			}
		}
	}

	if jsonFlag {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}

		fmt.Fprintln(stdout, string(data))

		return nil
	}

	// Render Terminal Output
	fmt.Fprintf(stdout, "%s\n", tui.Bold(tui.Cyan("⚡ Vortex Doctor — Workspace Health Diagnostic")))
	fmt.Fprintf(
		stdout,
		"Workspace: %s %s\n\n",
		rep.WorkspaceRoot,
		tui.Dim(fmt.Sprintf("(%s, Go %s)", rep.Platform, rep.GoVersion)),
	)

	maxNameWidth := 0
	for _, c := range rep.Checks {
		if len(c.Name) > maxNameWidth {
			maxNameWidth = len(c.Name)
		}
	}

	for _, check := range rep.Checks {
		badge := tui.BadgePassed()
		if !check.Passed {
			badge = tui.BadgeFail()
		} else if check.Warning {
			badge = tui.BadgeWarn()
		}

		fmt.Fprintf(stdout, "  %s  %-*s  %s\n", badge, maxNameWidth, check.Name, check.Message)

		if check.FixHint != "" && (!check.Passed || check.Warning) {
			fmt.Fprintf(stdout, "      %s  ↳ %s\n", strings.Repeat(" ", maxNameWidth), tui.Dim(check.FixHint))
		}
	}

	fmt.Fprintln(stdout)

	if rep.ErrorCount > 0 {
		fmt.Fprintf(
			stdout,
			"%s\n",
			tui.Red(fmt.Sprintf("❌ Doctor found %d issue(s) that require attention.", rep.ErrorCount)),
		)

		return errors.New("workspace doctor checks failed")
	}

	if rep.WarnCount > 0 {
		fmt.Fprintf(
			stdout,
			"%s\n",
			tui.Yellow(fmt.Sprintf("⚠️  Doctor found %d warning(s). Workspace is operational.", rep.WarnCount)),
		)
	} else {
		fmt.Fprintf(stdout, "%s\n", tui.Green("✨ All diagnostic checks passed. Workspace is in pristine condition!"))
	}

	return nil
}

func (r *DoctorReport) addCheck(c DoctorCheck) {
	switch {
	case !c.Passed:
		r.ErrorCount++
	case c.Warning:
		r.WarnCount++
	default:
		r.PassedCount++
	}

	r.Checks = append(r.Checks, c)
}
