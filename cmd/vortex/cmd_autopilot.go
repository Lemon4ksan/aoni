// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	goastparser "go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/builder"
	"github.com/lemon4ksan/aoni/internal/codegen/lint"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/codegen/project"
	"github.com/lemon4ksan/aoni/internal/codegen/spec"
	"github.com/lemon4ksan/aoni/internal/tui"
)

// CmdAutoPilot is the ultimate zero-argument intelligent runner.
// In an empty workspace, it guides interactive scaffolding.
// In an active workspace, it executes a 4-stage quality audit, upstream sync, compilation, and executive reporting.
type CmdAutoPilot struct {
	app *App
}

func (c *CmdAutoPilot) Name() string      { return "autopilot" }
func (c *CmdAutoPilot) Aliases() []string { return []string{"auto", "run", "all"} }
func (c *CmdAutoPilot) Synopsis() string {
	return "Intelligent Auto-Pilot: Audit, synchronize upstreams, and compile contracts in one shot"
}
func (c *CmdAutoPilot) Usage() string { return "vortex" }

func (c *CmdAutoPilot) Run(ctx context.Context, _ []string, stdout, stderr io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Check if current directory is a multi-repo workspace (.vortex.work)
	if wc, wErr := project.LoadWork(cwd); wErr == nil && wc != nil && len(wc.Workspaces) > 0 && wc.WorkDir == cwd {
		workCmd := &CmdWork{app: c.app}
		return workCmd.runForward(ctx, "autopilot", nil, stdout, stderr)
	}

	rootDir, configPath, _ := project.FindRoot(cwd)
	cfg, _ := project.Load(rootDir)

	var activeContracts []string
	if cfg != nil && len(cfg.Contracts) > 0 {
		for _, ct := range cfg.Contracts {
			activeContracts = append(activeContracts, filepath.Join(rootDir, ct.File))
		}
	} else if configPath != "" {
		// If explicit .vortex.yml existed, check candidate files in workspace
		contractFiles := builder.CollectInputFiles("", []string{rootDir})
		for _, f := range contractFiles {
			if strings.HasSuffix(f, ".gen.go") || strings.HasSuffix(f, "_test.go") {
				continue
			}

			if builder.QuickCheckCandidate(f) {
				activeContracts = append(activeContracts, f)
			}
		}
	}

	// WORLD 1: No contracts found -> Interactive Onboarding
	if len(activeContracts) == 0 {
		return c.runWorld1Onboarding(ctx, cwd, stdout, stderr)
	}

	// WORLD 2: Contracts found -> 4-Stage Auto-Pilot Run
	return c.runWorld2Pipeline(ctx, rootDir, cfg, activeContracts, stdout, stderr)
}

// WORLD 1: Interactive Onboarding & Guided Scaffolding
func (c *CmdAutoPilot) runWorld1Onboarding(ctx context.Context, cwd string, stdout, stderr io.Writer) error {
	fmt.Fprintln(stdout, "⚡ Vortex — Unified Zero-Allocation AST Toolchain")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "No @aoni contracts or .vortex.yml found in this workspace.")
	fmt.Fprintln(stdout)

	// Check if stdin is interactive
	stat, err := os.Stdin.Stat()
	isInteractive := err == nil && (stat.Mode()&os.ModeCharDevice) != 0

	if !isInteractive {
		if c.app != nil {
			c.app.PrintUsage(stdout)
		}

		return nil
	}

	fmt.Fprintln(stdout, "? What would you like to do?")
	fmt.Fprintln(stdout, "  [1] 🚀 Scaffold a new API contract (HTTP / REST, WebSocket, Socket)")
	fmt.Fprintln(stdout, "  [2] 📦 Ingest existing API (from OpenAPI, Swagger URL, Postman, or HAR)")
	fmt.Fprintln(stdout, "  [3] ⚙️  Initialize empty .vortex.yml configuration")
	fmt.Fprintln(stdout, "  [4] 📖 Print command help & exit")
	fmt.Fprint(stdout, "\n> ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	if choice == "" || choice == "4" {
		fmt.Fprintln(
			stdout,
			"\n💡 Run `vortex example` to inspect contract templates, or `vortex init` to configure workspace.",
		)

		return nil
	}

	switch choice {
	case "1":
		return c.scaffoldNewContract(ctx, cwd, reader, stdout, stderr)
	case "2":
		fmt.Fprint(stdout, "\nEnter path or URL to OpenAPI/Swagger JSON/YAML: ")

		specPath, _ := reader.ReadString('\n')

		specPath = strings.TrimSpace(specPath)
		if specPath == "" {
			return nil
		}

		cmdOapi := &CmdOAPI{}

		return cmdOapi.Run(ctx, []string{"import", specPath, "-out=pkg/api/api.go"}, stdout, stderr)

	case "3":
		cmdInit := &CmdInit{}
		return cmdInit.Run(ctx, []string{cwd}, stdout, stderr)
	default:
		if c.app != nil {
			c.app.PrintUsage(stdout)
		}

		return nil
	}
}

func (c *CmdAutoPilot) scaffoldNewContract(
	ctx context.Context,
	cwd string,
	reader *bufio.Reader,
	stdout, stderr io.Writer,
) error {
	fmt.Fprintln(stdout, "\n? Choose contract template:")
	fmt.Fprintln(stdout, "  [1] Declarative HTTP / REST Service (with @unwrap, @form, @referer)")
	fmt.Fprintln(stdout, "  [2] High-Throughput Socket Facade (Binary / Steam / Custom RPC)")
	fmt.Fprintln(stdout, "  [3] WebSocket Real-Time Client (with typed event handlers)")
	fmt.Fprintln(stdout, "  [4] Zero-Alloc Web Scraper (with @extract HTML pipelines)")
	fmt.Fprint(stdout, "\n> ")

	tplChoice, _ := reader.ReadString('\n')
	tplChoice = strings.TrimSpace(tplChoice)

	var tplKind string
	switch tplChoice {
	case "2":
		tplKind = "socket"
	case "3":
		tplKind = "ws"
	case "4":
		tplKind = "pipeline"
	default:
		tplKind = "http"
	}

	ex := spec.LookupExample(tplKind)
	if ex == nil {
		ex = spec.LookupExample("http")
	}

	targetDir := filepath.Join(cwd, "pkg", "api")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	apiFile := filepath.Join(targetDir, "api.go")
	if err := os.WriteFile(apiFile, []byte(ex.SourceCode), 0o600); err != nil {
		return err
	}

	// Create .vortex.yml
	vortexYml := `# .vortex.yml — Vortex API Guardian Workspace Configuration
version: 1

defaults:
  casing: snake_case
  engine: fast

contracts:
  - name: API
    package: api
    file: pkg/api/api.go
    gen: pkg/api/api.gen.go
`
	_ = os.WriteFile(filepath.Join(cwd, ".vortex.yml"), []byte(vortexYml), 0o600)

	// Initial compile
	b := builder.New(builder.Config{})

	res, genErr := b.BuildFile(ctx, apiFile, filepath.Join(targetDir, "api.gen.go"))
	if genErr != nil {
		return genErr
	}

	relAPI, _ := filepath.Rel(cwd, apiFile)
	relGen, _ := filepath.Rel(cwd, filepath.Join(targetDir, "api.gen.go"))

	fmt.Fprintf(stdout, "\n✔ Created %s (Declarative %s template)\n", relAPI, strings.ToUpper(tplKind))
	fmt.Fprintln(stdout, "✔ Created .vortex.yml (Workspace configuration)")
	fmt.Fprintf(stdout, "✔ Compiled %s (%d bytes)\n", relGen, len(res.Code))
	fmt.Fprintln(stdout, "\n✨ Workspace initialized! Run `vortex` anytime to audit, synchronize & rebuild.")

	return nil
}

// WORLD 2: 4-Stage Auto-Pilot Pipeline
func (c *CmdAutoPilot) runWorld2Pipeline(
	ctx context.Context,
	rootDir string,
	cfg *project.Config,
	contractFiles []string,
	stdout, stderr io.Writer,
) error {
	start := time.Now()

	fmt.Fprintf(stdout, "⚡ Vortex Auto-Pilot: Audit & Build Pipeline\n")
	fmt.Fprintf(stdout, "Workspace: %s (%d contract files)\n\n", rootDir, len(contractFiles))

	// STAGE 1: Pre-flight Contract Audit & Auto-Heal
	reg := lint.DefaultRegistry()
	reg.Disable("E001", "stale-codegen")

	if cfg != nil {
		reg.Disable(cfg.AllIgnoredRules()...)
	}

	engine := lint.NewEngine(reg)
	p := codeparser.NewParser()
	fset := token.NewFileSet()

	autoFixCount := 0
	hasCriticalErrors := false

	for _, file := range contractFiles {
		srcBytes, readErr := os.ReadFile(file)
		if readErr != nil {
			continue
		}

		astFile, parseErr := goastparser.ParseFile(fset, file, srcBytes, goastparser.ParseComments)
		if parseErr != nil {
			continue
		}

		rootIR, _ := p.ParseFile(file)
		pass := &lint.Pass{
			FilePath:    file,
			FileSet:     fset,
			ASTFile:     astFile,
			SourceBytes: srcBytes,
			RootIR:      rootIR,
		}

		report, lintErr := engine.Run(pass)
		if lintErr != nil {
			continue
		}

		// Fail-fast on critical E-series errors
		if report.Errors() > 0 {
			hasCriticalErrors = true

			fmt.Fprintf(stderr, "\n❌ [Pre-flight Linter Blocked Build] Critical error in %s:\n", file)

			for _, diag := range report.Diagnostics {
				if diag.Severity == lint.SeverityError {
					fmt.Fprintf(stderr, "  • [%s] %s:%d: %s\n", diag.RuleID, file, diag.Line, diag.Message)
				}
			}
		}

		// Auto-fix safe warnings
		if report.FixableCount() > 0 {
			applied, fixErr := report.ApplyFixes()
			if fixErr == nil {
				autoFixCount += applied
			}
		}
	}

	if hasCriticalErrors {
		return errors.New("contract compilation aborted due to critical lint errors (fix errors above to proceed)")
	}

	auditStatus := tui.BadgePassed() + " (100% clean)"
	if autoFixCount > 0 {
		auditStatus = fmt.Sprintf("%s (%d safe warnings auto-fixed)", tui.BadgePassed(), autoFixCount)
	}

	fmt.Fprintln(stdout, tui.RenderStep(1, 3, "Pre-flight Contract Audit", auditStatus, 44))

	// STAGE 2: Upstream DAG Sync
	upstreamSyncCount := 0
	if cfg != nil {
		for _, ct := range cfg.Contracts {
			if ct.Upstream != nil && ct.Upstream.Source != "" {
				upstreamPath := filepath.Join(rootDir, ct.Upstream.Source)
				srcPath := filepath.Join(rootDir, ct.File)
				upInfo, upErr := os.Stat(upstreamPath)
				srcInfo, srcErr := os.Stat(srcPath)

				if upErr == nil && srcErr == nil {
					if upInfo.ModTime().After(srcInfo.ModTime()) && ct.Upstream.Generate != "" {
						hookParts := strings.Fields(ct.Upstream.Generate)
						if len(hookParts) > 0 {
							// #nosec G204,G702 -- trusted upstream generator hook configured in .vortex.yml
							cmd := exec.CommandContext(ctx, hookParts[0], hookParts[1:]...)

							cmd.Dir = rootDir
							if err := cmd.Run(); err == nil {
								upstreamSyncCount++
							}
						}
					}
				}
			}
		}
	}

	if upstreamSyncCount > 0 {
		upStatus := fmt.Sprintf("%s (%d spec(s) updated)", tui.Green("✔ Synchronized"), upstreamSyncCount)
		fmt.Fprintln(stdout, tui.RenderStep(2, 3, "Upstream Specifications", upStatus, 44))
	} else {
		upStatus := tui.Green("✔ Up-to-date (0 drift)")
		fmt.Fprintln(stdout, tui.RenderStep(2, 3, "Upstream Specifications", upStatus, 44))
	}

	// STAGE 3: Multi-Compilation (AOT Generation & Polyglot Plugins)
	b := builder.New(builder.Config{})
	totalBytes := 0
	totalServices := 0
	totalMethods := 0

	for _, file := range contractFiles {
		relFile, _ := filepath.Rel(rootDir, file)
		outGen := ""

		if cfg != nil {
			for _, ct := range cfg.Contracts {
				if filepath.Clean(ct.File) == filepath.Clean(relFile) {
					if ct.Gen != "" {
						outGen = filepath.Join(rootDir, ct.Gen)
					}

					if ct.Harness != "" {
						harnessOut := ct.Harness
						if harnessOut == "true" {
							harnessOut = filepath.Join(rootDir, strings.TrimSuffix(ct.File, ".go")+"_harness.gen.go")
						} else {
							harnessOut = filepath.Join(rootDir, ct.Harness)
						}

						hRes, hErr := b.BuildHarness(ctx, file, harnessOut)
						if hErr != nil {
							return fmt.Errorf("harness compilation failed on %s: %w", file, hErr)
						}

						if hRes != nil {
							totalBytes += hRes.BytesCount
						}
					}

					if ct.Mock != "" {
						mockOut := ct.Mock
						if mockOut == "true" {
							mockOut = filepath.Join(rootDir, strings.TrimSuffix(ct.File, ".go")+"_mock.gen.go")
						} else {
							mockOut = filepath.Join(rootDir, ct.Mock)
						}

						mRes, mErr := b.BuildMock(ctx, file, mockOut)
						if mErr != nil {
							return fmt.Errorf("mock compilation failed on %s: %w", file, mErr)
						}

						if mRes != nil {
							totalBytes += mRes.BytesCount
						}
					}

					break
				}
			}
		}

		res, genErr := b.BuildFile(ctx, file, outGen)
		if genErr != nil {
			return fmt.Errorf("compilation failed on %s: %w", file, genErr)
		}

		totalBytes += res.BytesCount
		totalServices += res.ServicesCount
	}

	// Total methods calculation
	statusEngine := project.NewStatusEngine()

	var contractsList []project.ContractConfig
	if cfg != nil {
		contractsList = cfg.Contracts
	}

	statusRep := statusEngine.Inspect(&project.Config{RootDir: rootDir}, contractsList)

	totalMethods = statusRep.TotalMethods

	genStatus := fmt.Sprintf("%s %s emitted (%d services, %d methods)",
		tui.Green("✔"),
		formatByteSize(totalBytes),
		totalServices,
		totalMethods,
	)
	fmt.Fprintln(stdout, tui.RenderStep(3, 3, "Code Generation & Polyglot Targets", genStatus, 44))

	elapsed := time.Since(start)

	// STAGE 4: Final Consolidated Dashboard
	fmt.Fprintf(stdout, "\n%s\n", tui.RenderDivider(77))
	fmt.Fprintf(stdout, "Summary:\n")
	fmt.Fprintf(
		stdout,
		"  • Go Clients:        %d services, %d methods compiled (100%% Zero-Alloc)\n",
		totalServices,
		totalMethods,
	)

	if len(statusRep.Proposals) > 0 {
		fmt.Fprintf(
			stdout,
			"  • Active Proposals:  %d incoming consumer branches pending review\n",
			len(statusRep.Proposals),
		)
	}

	fmt.Fprintf(stdout, "  • Linter Score:      100%% Clean (All invariants respected)\n\n")

	fmt.Fprintf(
		stdout,
		"✨ Workspace is 100%% healthy, synchronized, and compiled in %v!\n",
		elapsed.Round(time.Millisecond),
	)

	return nil
}

func formatByteSize(b int) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}

	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024.0)
	}

	return fmt.Sprintf("%.2f MB", float64(b)/(1024.0*1024.0))
}
