// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/diff"
	"github.com/lemon4ksan/aoni/internal/codegen/ingest"
	"github.com/lemon4ksan/aoni/internal/codegen/openapi"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// PluginStatus reports the synchronization health of an external polyglot target.
type PluginStatus struct {
	Name    string `json:"name"`
	Out     string `json:"out"`
	IsStale bool   `json:"is_stale"`
	Message string `json:"message,omitempty"`
}

// ContractStatus represents the comprehensive health of a single service interface.
type ContractStatus struct {
	Name                  string         `json:"name"`
	Package               string         `json:"package"`
	File                  string         `json:"file"`
	GenFile               string         `json:"gen_file,omitempty"`
	ModelsFile            string         `json:"models_file,omitempty"`
	MethodsCount          int            `json:"methods_count"`
	DTOsCount             int            `json:"dtos_count"`
	Version               string         `json:"version,omitempty"`
	Source                string         `json:"source,omitempty"`
	IsGenStale            bool           `json:"is_gen_stale"`
	GenStaleReason        string         `json:"gen_stale_reason,omitempty"`
	UpstreamDriftCount    int            `json:"upstream_drift_count"`
	UpstreamBreakingCount int            `json:"upstream_breaking_count"`
	UpstreamGhostCount    int            `json:"upstream_ghost_count"`
	Plugins               []PluginStatus `json:"plugins,omitempty"`
}

// StatusReport summarizes the health of all monitored contracts across the workspace.
type StatusReport struct {
	WorkspaceRoot string           `json:"workspace_root"`
	ConfigPath    string           `json:"config_path,omitempty"`
	Contracts     []ContractStatus `json:"contracts"`
	TotalMethods  int              `json:"total_methods"`
	TotalDTOs     int              `json:"total_dtos"`
	NextActions   []string         `json:"next_actions,omitempty"`
}

// StaleCount returns the number of contracts requiring code generation.
func (r *StatusReport) StaleCount() int {
	count := 0
	for _, c := range r.Contracts {
		if c.IsGenStale {
			count++
		}
	}

	return count
}

// BreakingDriftCount returns the number of breaking changes across all upstream specs.
func (r *StatusReport) BreakingDriftCount() int {
	count := 0
	for _, c := range r.Contracts {
		count += c.UpstreamBreakingCount
	}

	return count
}

// HasIssues returns true if any contract is stale or has breaking upstream changes.
func (r *StatusReport) HasIssues() bool {
	return r.StaleCount() > 0 || r.BreakingDriftCount() > 0
}

// Render formats a colored, human-readable terminal dashboard.
func (r *StatusReport) Render(color bool) string {
	var sb strings.Builder
	sb.WriteString("⚡ Vortex API Guardian\n")
	fmt.Fprintf(&sb, "Workspace: %s (%d services, %d methods)\n\n", r.WorkspaceRoot, len(r.Contracts), r.TotalMethods)

	if len(r.Contracts) == 0 {
		sb.WriteString("No service contracts detected. Run `vortex init` to configure workspace.\n")
		return sb.String()
	}

	sb.WriteString("● Contracts & Generated Code:\n")

	for _, c := range r.Contracts {
		statusIcon := "✔"
		statusDesc := "100% in sync"

		if c.IsGenStale {
			statusIcon = "⚠"
			statusDesc = c.GenStaleReason
		}

		verInfo := ""
		if c.Version != "" {
			verInfo = fmt.Sprintf("[%s]", c.Version)
		}

		dtoInfo := ""
		if c.DTOsCount > 0 {
			dtoInfo = fmt.Sprintf(", %d DTOs", c.DTOsCount)
		}

		fmt.Fprintf(&sb, "  %s %-10s (%-22s) %2d methods%s  %s %s\n",
			statusIcon,
			c.Name,
			filepath.ToSlash(filepath.Dir(c.File)),
			c.MethodsCount,
			dtoInfo,
			verInfo,
			statusDesc)
	}

	sb.WriteString("\n")

	// Upstream Drift section
	hasUpstream := false
	for _, c := range r.Contracts {
		if c.Source != "" {
			hasUpstream = true
			break
		}
	}

	if hasUpstream {
		sb.WriteString("● Upstream Drift (OpenAPI / External):\n")

		for _, c := range r.Contracts {
			if c.Source == "" {
				continue
			}

			switch {
			case c.UpstreamBreakingCount > 0:
				fmt.Fprintf(
					&sb,
					"  🔴 %s: %d BREAKING drift(s) detected with %s\n",
					c.Name,
					c.UpstreamBreakingCount,
					c.Source,
				)

			case c.UpstreamDriftCount > 0 || c.UpstreamGhostCount > 0:
				fmt.Fprintf(
					&sb,
					"  🟡 %s: %d non-breaking update(s) available in %s\n",
					c.Name,
					c.UpstreamDriftCount+c.UpstreamGhostCount,
					c.Source,
				)

			default:
				fmt.Fprintf(&sb, "  ✔ %s: Up-to-date with %s (0 drift)\n", c.Name, c.Source)
			}
		}

		sb.WriteString("\n")
	}

	// Polyglot plugins
	hasPlugins := false
	for _, c := range r.Contracts {
		if len(c.Plugins) > 0 {
			hasPlugins = true
			break
		}
	}

	if hasPlugins {
		sb.WriteString("● Polyglot Targets:\n")

		for _, c := range r.Contracts {
			for _, p := range c.Plugins {
				icon := "✔"

				status := "Up to date"
				if p.IsStale {
					icon = "⚠"
					status = "Stale (rebuild required)"
				}

				fmt.Fprintf(&sb, "  %s %s SDK (%s) ... %s\n", icon, strings.ToUpper(p.Name), p.Out, status)
			}
		}

		sb.WriteString("\n")
	}

	if len(r.NextActions) > 0 {
		sb.WriteString("───────────────────────────────────────────────────────────────────\n")
		sb.WriteString("Next Actions:\n")

		for _, action := range r.NextActions {
			fmt.Fprintf(&sb, "  ↳ %s\n", action)
		}
	} else {
		sb.WriteString("✨ All systems nominal. Network layer is 100% synchronized.\n")
	}

	return sb.String()
}

// RenderJSON serializes the status report into JSON bytes.
func (r *StatusReport) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// StatusEngine inspects workspace contracts, freshness, and upstream drift.
type StatusEngine struct{}

// NewStatusEngine creates an initialized StatusEngine instance.
func NewStatusEngine() *StatusEngine {
	return &StatusEngine{}
}

// Inspect evaluates contracts in the workspace against disk state and upstream sources.
func (e *StatusEngine) Inspect(cfg *Config, contracts []ContractConfig) *StatusReport {
	report := &StatusReport{
		WorkspaceRoot: cfg.RootDir,
		ConfigPath:    cfg.ConfigPath,
		Contracts:     make([]ContractStatus, 0, len(contracts)),
		NextActions:   make([]string, 0),
	}

	p := parser.NewParser()
	diffEngine := diff.NewEngine()

	var (
		staleServiceNames []string
		driftServiceNames []string
	)

	for _, ct := range contracts {
		absFile := filepath.Join(cfg.RootDir, ct.File)

		srcInfo, err := os.Stat(absFile)
		if err != nil {
			continue
		}

		root, err := p.ParseFile(absFile)
		if err != nil || len(root.Services) == 0 {
			continue
		}

		totalMethods := 0
		for _, s := range root.Services {
			totalMethods += len(s.Methods)
		}

		svcName := ct.Name
		if svcName == "" {
			svcName = root.Services[0].Name
			if len(root.Services) > 1 {
				svcName = fmt.Sprintf("%s (+%d)", root.Services[0].Name, len(root.Services)-1)
			}
		}

		status := ContractStatus{
			Name:         svcName,
			Package:      root.PackageName,
			File:         ct.File,
			GenFile:      ct.Gen,
			ModelsFile:   ct.Models,
			MethodsCount: totalMethods,
			DTOsCount:    len(root.Structs),
			Version:      root.Services[0].Version,
			Source:       root.Services[0].Source,
		}

		if ct.Upstream != nil && ct.Upstream.Source != "" && status.Source == "" {
			status.Source = ct.Upstream.Source
		}

		report.TotalMethods += status.MethodsCount
		report.TotalDTOs += status.DTOsCount

		// 1. Check generated code freshness
		if ct.Gen != "" {
			absGen := filepath.Join(cfg.RootDir, ct.Gen)

			genInfo, genErr := os.Stat(absGen)
			if genErr != nil {
				status.IsGenStale = true
				status.GenStaleReason = "api.gen.go is missing"
			} else if srcInfo.ModTime().After(genInfo.ModTime()) {
				status.IsGenStale = true
				status.GenStaleReason = "api.gen.go is STALE"
			}
		}

		if !status.IsGenStale && ct.Models != "" {
			absModels := filepath.Join(cfg.RootDir, ct.Models)

			modelsInfo, modelsErr := os.Stat(absModels)
			if modelsErr == nil && srcInfo.ModTime().After(modelsInfo.ModTime()) {
				status.IsGenStale = true
				status.GenStaleReason = "models.gen.go is STALE"
			} else if modelsErr != nil && cfg.ConfigPath != "" {
				status.IsGenStale = true
				status.GenStaleReason = "models.gen.go is missing"
			}
		}

		if status.IsGenStale {
			staleServiceNames = append(staleServiceNames, status.Name)
		}

		// 2. Check upstream drift if source is available
		if status.Source != "" {
			upstreamPath := status.Source
			if !filepath.IsAbs(upstreamPath) && !strings.HasPrefix(upstreamPath, "http://") &&
				!strings.HasPrefix(upstreamPath, "https://") {
				upstreamPath = filepath.Join(cfg.RootDir, upstreamPath)
			}

			rawSpecBytes, readErr := os.ReadFile(upstreamPath)
			specFormat, _ := ingest.DetectFormat(rawSpecBytes)

			if readErr == nil && (specFormat == ingest.FormatOpenAPI3 || specFormat == ingest.FormatSwagger2) {
				if doc, docErr := openapi.LoadSpec(upstreamPath, nil); docErr == nil {
					diffReport := diffEngine.Compare(root, doc, ct.File, status.Source)
					status.UpstreamBreakingCount = diffReport.BreakingCount()
					status.UpstreamDriftCount = diffReport.NonBreakingCount()
					status.UpstreamGhostCount = diffReport.GhostCount()

					if status.UpstreamBreakingCount > 0 || status.UpstreamDriftCount > 0 {
						driftServiceNames = append(
							driftServiceNames,
							fmt.Sprintf("%s (%s)", status.Name, status.Source),
						)
					}
				}
			} else if upInfo, upErr := os.Stat(upstreamPath); upErr == nil {
				// If not standard OpenAPI (e.g. proprietary JSON/YAML dump), check file modification timestamp
				if upInfo.ModTime().After(srcInfo.ModTime()) {
					status.UpstreamDriftCount = 1
					if ct.Upstream != nil && ct.Upstream.Generate != "" {
						report.NextActions = append(
							report.NextActions,
							fmt.Sprintf(
								"Run `%s` to rebuild contract from updated upstream dump (%s)",
								ct.Upstream.Generate,
								status.Source,
							),
						)
					} else {
						driftServiceNames = append(
							driftServiceNames,
							fmt.Sprintf("%s (%s)", status.Name, status.Source),
						)
					}
				}
			}
		}

		// 3. Check polyglot plugins
		for _, pl := range ct.Plugins {
			absPlOut := filepath.Join(cfg.RootDir, pl.Out)
			plInfo, plErr := os.Stat(absPlOut)

			pStatus := PluginStatus{
				Name:    pl.Name,
				Out:     pl.Out,
				IsStale: false,
			}
			if plErr != nil || srcInfo.ModTime().After(plInfo.ModTime()) {
				pStatus.IsStale = true
			}

			status.Plugins = append(status.Plugins, pStatus)
		}

		report.Contracts = append(report.Contracts, status)
	}

	// Build actionable NextActions
	if len(staleServiceNames) > 0 {
		report.NextActions = append(report.NextActions,
			fmt.Sprintf("Run `vortex gen` to rebuild stale Go code (%s)", strings.Join(staleServiceNames, ", ")),
		)
	}

	if len(driftServiceNames) > 0 {
		report.NextActions = append(
			report.NextActions,
			fmt.Sprintf(
				"Run `vortex oapi import` to reconcile upstream changes (%s)",
				strings.Join(driftServiceNames, ", "),
			),
		)
	}

	return report
}
