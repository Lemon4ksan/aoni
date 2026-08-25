// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lemon4ksan/foundation/pathkit"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/diff"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/git"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ingest"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/openapi"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

// PluginStatus reports the synchronization health of an external polyglot target.
type PluginStatus struct {
	Name    string `json:"name"`
	Out     string `json:"out"`
	IsStale bool   `json:"is_stale"`
	Message string `json:"message,omitempty"`
}

// ContractStatus captures the integrity and freshness state of an individual contract.
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
	UpstreamBreakingCount int            `json:"upstream_breaking_count"`
	UpstreamDriftCount    int            `json:"upstream_drift_count"`
	UpstreamGhostCount    int            `json:"upstream_ghost_count"`
	Plugins               []PluginStatus `json:"plugins,omitempty"`
}

// StatusReport summarizes the health of all monitored contracts across the workspace.
type StatusReport struct {
	WorkspaceRoot string               `json:"workspace_root"`
	ConfigPath    string               `json:"config_path,omitempty"`
	Contracts     []ContractStatus     `json:"contracts"`
	Proposals     []git.BranchProposal `json:"proposals,omitempty"`
	TotalMethods  int                  `json:"total_methods"`
	TotalDTOs     int                  `json:"total_dtos"`
	NextActions   []string             `json:"next_actions,omitempty"`
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
	if color && (os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb") {
		color = false
	}

	var sb strings.Builder
	sb.WriteString(ansiBold(color, ansiCyan(color, "⚡ Vortex API Guardian")) + "\n")
	fmt.Fprintf(&sb, "Workspace: %s %s\n\n",
		r.WorkspaceRoot,
		ansiDim(color, fmt.Sprintf("(%d services, %d methods)", len(r.Contracts), r.TotalMethods)),
	)

	if len(r.Contracts) == 0 {
		sb.WriteString("No service contracts detected. Run `vortex init` to configure workspace.\n")
		return sb.String()
	}

	sb.WriteString(ansiBold(color, "● Contracts & Generated Code:") + "\n")

	maxNameWidth := 0
	maxPathWidth := 0
	maxMethodsDigits := 1
	maxDTOsDigits := 1
	hasAnyDTOs := false
	hasAnyVersion := false
	maxVerWidth := 0

	for _, c := range r.Contracts {
		if len(c.Name) > maxNameWidth {
			maxNameWidth = len(c.Name)
		}

		pathStr := "(" + filepath.ToSlash(filepath.Dir(c.File)) + ")"
		if len(pathStr) > maxPathWidth {
			maxPathWidth = len(pathStr)
		}

		mDigits := len(strconv.Itoa(c.MethodsCount))
		if mDigits > maxMethodsDigits {
			maxMethodsDigits = mDigits
		}

		if c.DTOsCount > 0 {
			hasAnyDTOs = true

			dDigits := len(strconv.Itoa(c.DTOsCount))
			if dDigits > maxDTOsDigits {
				maxDTOsDigits = dDigits
			}
		}

		if c.Version != "" {
			hasAnyVersion = true

			verLen := len(fmt.Sprintf("[%s]", c.Version))
			if verLen > maxVerWidth {
				maxVerWidth = verLen
			}
		}
	}

	for _, c := range r.Contracts {
		statusIcon := "✔"
		statusDesc := "100% in sync"

		if c.IsGenStale {
			statusIcon = "⚠"
			statusDesc = c.GenStaleReason
		}

		iconStyled := statusIcon
		if color {
			if c.IsGenStale {
				iconStyled = ansiYellow(color, statusIcon)
			} else {
				iconStyled = ansiGreen(color, statusIcon)
			}
		}

		namePadded := fmt.Sprintf("%-*s", maxNameWidth, c.Name)

		pathStr := "(" + filepath.ToSlash(filepath.Dir(c.File)) + ")"

		pathPadded := fmt.Sprintf("%-*s", maxPathWidth, pathStr)
		if color {
			pathPadded = ansiDim(color, pathPadded)
		}

		methodsPart := fmt.Sprintf("%*d methods", maxMethodsDigits, c.MethodsCount)

		var metricsPart string
		if hasAnyDTOs {
			if c.DTOsCount > 0 {
				metricsPart = fmt.Sprintf("%s, %*d DTOs", methodsPart, maxDTOsDigits, c.DTOsCount)
			} else {
				metricsPart = methodsPart + strings.Repeat(" ", 2+maxDTOsDigits+5)
			}
		} else {
			metricsPart = methodsPart
		}

		descStyled := statusDesc
		if color {
			if c.IsGenStale {
				descStyled = ansiYellow(color, statusDesc)
			} else {
				descStyled = ansiGreen(color, statusDesc)
			}
		}

		if hasAnyVersion {
			verStr := ""
			if c.Version != "" {
				verStr = fmt.Sprintf("[%s]", c.Version)
			}

			verPadded := fmt.Sprintf("%-*s", maxVerWidth, verStr)
			if color && verStr != "" {
				verPadded = ansiCyan(color, verStr) + strings.Repeat(" ", maxVerWidth-len(verStr))
			}

			fmt.Fprintf(&sb, "  %s %s  %s  %s  %s  %s\n",
				iconStyled,
				namePadded,
				pathPadded,
				metricsPart,
				verPadded,
				descStyled,
			)
		} else {
			fmt.Fprintf(&sb, "  %s %s  %s  %s  %s\n",
				iconStyled,
				namePadded,
				pathPadded,
				metricsPart,
				descStyled,
			)
		}
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
		sb.WriteString(ansiBold(color, "● Upstream Drift (OpenAPI / External):") + "\n")

		maxUpstreamNameWidth := 0
		for _, c := range r.Contracts {
			if c.Source != "" && len(c.Name) > maxUpstreamNameWidth {
				maxUpstreamNameWidth = len(c.Name)
			}
		}

		for _, c := range r.Contracts {
			if c.Source == "" {
				continue
			}

			namePadded := fmt.Sprintf("%-*s", maxUpstreamNameWidth, c.Name)

			switch {
			case c.UpstreamBreakingCount > 0:
				desc := fmt.Sprintf("%d BREAKING drift(s) detected with %s", c.UpstreamBreakingCount, c.Source)
				if color {
					desc = ansiRed(color, desc)
				}

				fmt.Fprintf(&sb, "  🔴 %s  %s\n", namePadded, desc)

			case c.UpstreamDriftCount > 0 || c.UpstreamGhostCount > 0:
				desc := fmt.Sprintf(
					"%d non-breaking update(s) available in %s",
					c.UpstreamDriftCount+c.UpstreamGhostCount,
					c.Source,
				)
				if color {
					desc = ansiYellow(color, desc)
				}

				fmt.Fprintf(&sb, "  🟡 %s  %s\n", namePadded, desc)

			default:
				icon := "✔"
				if color {
					icon = ansiGreen(color, "✔")
				}

				desc := fmt.Sprintf("Up-to-date with %s (0 drift)", c.Source)
				if color {
					desc = ansiGreen(color, desc)
				}

				fmt.Fprintf(&sb, "  %s  %s  %s\n", icon, namePadded, desc)
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
		sb.WriteString(ansiBold(color, "● Polyglot Targets:") + "\n")

		maxTargetName := 0

		maxTargetOut := 0
		for _, c := range r.Contracts {
			for _, p := range c.Plugins {
				targetName := strings.ToUpper(p.Name) + " SDK"
				if len(targetName) > maxTargetName {
					maxTargetName = len(targetName)
				}

				outStr := "(" + p.Out + ")"
				if len(outStr) > maxTargetOut {
					maxTargetOut = len(outStr)
				}
			}
		}

		for _, c := range r.Contracts {
			for _, p := range c.Plugins {
				icon := "✔"

				status := "Up to date"
				if p.IsStale {
					icon = "⚠"
					status = "Stale (rebuild required)"
				}

				iconStyled := icon

				statusStyled := status
				if color {
					if p.IsStale {
						iconStyled = ansiYellow(color, "⚠")
						statusStyled = ansiYellow(color, status)
					} else {
						iconStyled = ansiGreen(color, "✔")
						statusStyled = ansiGreen(color, status)
					}
				}

				targetName := strings.ToUpper(p.Name) + " SDK"
				targetPadded := fmt.Sprintf("%-*s", maxTargetName, targetName)

				outStr := "(" + p.Out + ")"

				outPadded := fmt.Sprintf("%-*s", maxTargetOut, outStr)
				if color {
					outPadded = ansiDim(color, outPadded)
				}

				fmt.Fprintf(&sb, "  %s %s  %s  %s\n", iconStyled, targetPadded, outPadded, statusStyled)
			}
		}

		sb.WriteString("\n")
	}

	if len(r.Proposals) > 0 {
		sb.WriteString(ansiBold(color, "● Incoming Consumer Proposals (Git Branches):") + "\n")

		maxPropName := 0

		maxAuthor := 0
		for _, prop := range r.Proposals {
			if len(prop.Name) > maxPropName {
				maxPropName = len(prop.Name)
			}

			author := "@" + prop.Author
			if len(author) > maxAuthor {
				maxAuthor = len(author)
			}
		}

		for _, prop := range r.Proposals {
			remoteTag := ""
			if prop.IsRemote {
				remoteTag = " [remote]"
				if color {
					remoteTag = ansiCyan(color, " [remote]")
				}
			}

			propPadded := fmt.Sprintf("%-*s", maxPropName, prop.Name)
			author := "@" + prop.Author
			authorPadded := fmt.Sprintf("%-*s", maxAuthor, author)

			dateStr := fmt.Sprintf("(%s)", prop.Date)
			if color {
				dateStr = ansiDim(color, dateStr)
			}

			fmt.Fprintf(&sb, "  🔵 %s  by %s  %s%s\n", propPadded, authorPadded, dateStr, remoteTag)
		}

		sb.WriteString("\n")
	}

	if len(r.NextActions) > 0 {
		sb.WriteString(ansiDim(color, "───────────────────────────────────────────────────────────────────") + "\n")
		sb.WriteString(ansiBold(color, ansiYellow(color, "Next Actions:")) + "\n")

		for _, action := range r.NextActions {
			arrow := "↳"
			if color {
				arrow = ansiCyan(color, "↳")
			}

			fmt.Fprintf(&sb, "  %s %s\n", arrow, action)
		}
	} else {
		msg := "✨ All systems nominal. Network layer is 100% synchronized.\n"
		if color {
			msg = ansiBold(color, ansiGreen(color, "✨ All systems nominal. Network layer is 100% synchronized.\n"))
		}

		sb.WriteString(msg)
	}

	return sb.String()
}

func ansi(color bool, code, text string) string {
	if !color || text == "" {
		return text
	}

	return code + text + "\033[0m"
}

func ansiBold(color bool, text string) string   { return ansi(color, "\033[1m", text) }
func ansiDim(color bool, text string) string    { return ansi(color, "\033[2m", text) }
func ansiGreen(color bool, text string) string  { return ansi(color, "\033[32m", text) }
func ansiYellow(color bool, text string) string { return ansi(color, "\033[33m", text) }
func ansiRed(color bool, text string) string    { return ansi(color, "\033[31m", text) }
func ansiCyan(color bool, text string) string   { return ansi(color, "\033[36m", text) }

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
			pSrc := pathkit.New(status.Source)

			upstreamPath := pSrc.FilePath()
			if pSrc.IsFile() && !pSrc.IsAbs() {
				upstreamPath = filepath.Join(cfg.RootDir, upstreamPath)
			}

			rawSpecBytes, readErr := os.ReadFile(upstreamPath)
			specFormat, _ := ingest.DetectFormat(rawSpecBytes)

			if readErr == nil && (specFormat == ingest.FormatOpenAPI3 || specFormat == ingest.FormatSwagger2) {
				if doc, docErr := openapi.LoadSpec(upstreamPath, nil); docErr == nil {
					diffReport := diff.Compare(root, doc, ct.File, status.Source)
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

	// 4. Discover incoming proposals from Git branches
	if proposals, pErr := git.ListProposalBranches(
		context.Background(),
		cfg.RootDir,
		nil,
	); pErr == nil &&
		len(proposals) > 0 {
		report.Proposals = proposals
		for _, prop := range proposals {
			report.NextActions = append(
				report.NextActions,
				fmt.Sprintf(
					"Run `vortex review %s` or `vortex accept %s` to inspect/merge proposal",
					prop.Name,
					prop.Name,
				),
			)
		}
	}

	return report
}
