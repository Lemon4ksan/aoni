// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lemon4ksan/aoni/internal/version"
)

// ANSI color codes
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
)

// FormatReport writes a formatted terminal report of discovered diagnostics.
func FormatReport(w io.Writer, target string, report *Report) {
	if report == nil {
		return
	}

	fmt.Fprintf(w, "%s%s⚡ Vortex Contract Inspector%s\n", ansiBold, ansiCyan, ansiReset)
	fmt.Fprintf(w, "%sTarget: %s (%d services, %d methods across %d files)%s\n\n",
		ansiDim, target, report.ServicesChecked, report.MethodsChecked, report.FilesChecked, ansiReset)

	if len(report.Diagnostics) == 0 {
		if report.SuppressedCount > 0 {
			fmt.Fprintf(
				w,
				"%s%s✔ All contracts are valid and synchronized!%s %s(%d warnings suppressed via //vortex:ignore)%s\n",
				ansiBold,
				ansiGreen,
				ansiReset,
				ansiDim,
				report.SuppressedCount,
				ansiReset,
			)
		} else {
			fmt.Fprintf(w, "%s%s✔ All contracts are valid and synchronized!%s\n", ansiBold, ansiGreen, ansiReset)
		}

		return
	}

	// Group by severity
	var errs, warns, infos []Diagnostic

	for _, d := range report.Diagnostics {
		switch d.Severity {
		case SeverityError:
			errs = append(errs, d)
		case SeverityWarning:
			warns = append(warns, d)
		default:
			infos = append(infos, d)
		}
	}

	sortDiagnostics(errs)
	sortDiagnostics(warns)
	sortDiagnostics(infos)

	if len(errs) > 0 {
		fmt.Fprintf(w, "%s%s◆ Errors (%d)%s\n", ansiBold, ansiRed, len(errs), ansiReset)

		for _, d := range errs {
			printDiagnostic(w, d, ansiRed)
		}

		fmt.Fprintln(w)
	}

	if len(warns) > 0 {
		fmt.Fprintf(w, "%s%s◆ Warnings & Suggestions (%d)%s\n", ansiBold, ansiYellow, len(warns), ansiReset)

		for _, d := range warns {
			printDiagnostic(w, d, ansiYellow)
		}

		fmt.Fprintln(w)
	}

	if len(infos) > 0 {
		fmt.Fprintf(w, "%s%s◆ Info (%d)%s\n", ansiBold, ansiBlue, len(infos), ansiReset)

		for _, d := range infos {
			printDiagnostic(w, d, ansiBlue)
		}

		fmt.Fprintln(w)
	}

	// Summary with colored severity breakdown and rule count list
	fmt.Fprintf(w, "%sSummary:%s ", ansiBold, ansiReset)

	var parts []string
	if report.Errors() > 0 {
		parts = append(parts, fmt.Sprintf("%s%d error(s)%s", ansiRed, report.Errors(), ansiReset))
	}

	if report.Warnings() > 0 {
		parts = append(parts, fmt.Sprintf("%s%d warning(s)%s", ansiYellow, report.Warnings(), ansiReset))
	}

	if report.FixableCount() > 0 {
		parts = append(parts, fmt.Sprintf("%s%d auto-fixable%s", ansiGreen, report.FixableCount(), ansiReset))
	}

	if report.SuppressedCount > 0 {
		parts = append(parts, fmt.Sprintf("%s%d suppressed%s", ansiDim, report.SuppressedCount, ansiReset))
	}

	fmt.Fprintln(w, strings.Join(parts, ", "))

	type ruleStat struct {
		ruleKey string
		count   int
	}

	ruleMap := make(map[string]int)
	for _, d := range report.Diagnostics {
		key := fmt.Sprintf("%s (%s)", d.RuleID, d.RuleName)
		ruleMap[key]++
	}

	var stats []ruleStat
	for k, v := range ruleMap {
		stats = append(stats, ruleStat{ruleKey: k, count: v})
	}

	slices.SortFunc(stats, func(a, b ruleStat) int {
		if a.count != b.count {
			return cmp.Compare(b.count, a.count)
		}

		return cmp.Compare(a.ruleKey, b.ruleKey)
	})

	for _, s := range stats {
		fmt.Fprintf(w, "* %s: %d\n", s.ruleKey, s.count)
	}

	if report.FixableCount() > 0 {
		fmt.Fprintf(w, "\n%sRun `vortex check --fix` to automatically resolve %d safe issue(s).%s\n",
			ansiCyan, report.FixableCount(), ansiReset)
	}
}

func sortDiagnostics(diags []Diagnostic) {
	slices.SortStableFunc(diags, func(a, b Diagnostic) int {
		return cmp.Or(
			cmp.Compare(a.RuleID, b.RuleID),
			cmp.Compare(a.FilePath, b.FilePath),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Column, b.Column),
		)
	})
}

func printDiagnostic(w io.Writer, d Diagnostic, color string) {
	line := d.Line
	if line <= 0 {
		line = 1
	}

	col := d.Column
	if col <= 0 {
		col = 1
	}

	filePath := d.FilePath
	if abs, err := filepath.Abs(filePath); err == nil {
		filePath = abs
	}

	loc := fmt.Sprintf("%s:%d:%d", filePath, line, col)

	fmt.Fprintf(w, "  ↳ %s[%s:%s]%s %s%s%s\n", color, d.RuleID, d.RuleName, ansiReset, ansiBold, loc, ansiReset)
	fmt.Fprintf(w, "    %s\n", d.Message)

	if d.Suggestion != "" && !strings.Contains(d.Suggestion, "vortex check --fix") {
		fmt.Fprintf(w, "    %s↳ Suggestion:%s %s\n", ansiCyan, ansiReset, d.Suggestion)
	}

	if !d.Fixable() {
		fmt.Fprintf(w, "    %s↳ To suppress:%s //vortex:ignore %s\n", ansiDim, ansiReset, d.RuleName)
	}
}

func (d Diagnostic) Fixable() bool {
	return d.Fix != nil
}

// FormatGitHubActions outputs diagnostics as GitHub Actions Workflow Command annotations.
// Format: ::error file={name},line={line},title={title}::{message}
func FormatGitHubActions(w io.Writer, report *Report) {
	if report == nil {
		return
	}

	for _, d := range report.Diagnostics {
		level := "warning"
		switch d.Severity {
		case SeverityError:
			level = "error"
		case SeverityInfo:
			level = "notice"
		}

		filePath := filepath.ToSlash(d.FilePath)

		line := d.Line
		if line <= 0 {
			line = 1
		}

		fmt.Fprintf(w, "::%s file=%s,line=%d,title=%s::[%s] %s\n",
			level, filePath, line, d.RuleID, d.RuleName, d.Message)
	}
}

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description sarifMsgWrapper `json:"shortDescription"`
}

type sarifResult struct {
	RuleID    string             `json:"ruleId"`
	Level     string             `json:"level"`
	Message   sarifMsgWrapper    `json:"message"`
	Locations []sarifLocationObj `json:"locations"`
}

type sarifMsgWrapper struct {
	Text string `json:"text"`
}

type sarifLocationObj struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// FormatSARIF outputs diagnostics adhering to OASIS SARIF v2.1.0 JSON format.
func FormatSARIF(w io.Writer, report *Report) error {
	if report == nil {
		return nil
	}

	ruleMap := make(map[string]bool)

	var (
		rules   []sarifRule
		results []sarifResult
	)

	for _, d := range report.Diagnostics {
		if !ruleMap[d.RuleID] {
			ruleMap[d.RuleID] = true
			rules = append(rules, sarifRule{
				ID:          d.RuleID,
				Name:        d.RuleName,
				Description: sarifMsgWrapper{Text: d.RuleName},
			})
		}

		level := "warning"
		switch d.Severity {
		case SeverityError:
			level = "error"
		case SeverityInfo:
			level = "note"
		}

		line := d.Line
		if line <= 0 {
			line = 1
		}

		results = append(results, sarifResult{
			RuleID:  d.RuleID,
			Level:   level,
			Message: sarifMsgWrapper{Text: d.Message},
			Locations: []sarifLocationObj{
				{
					PhysicalLocation: sarifPhysicalLocation{
						ArtifactLocation: sarifArtifact{
							URI: filepath.ToSlash(d.FilePath),
						},
						Region: sarifRegion{
							StartLine: line,
						},
					},
				},
			},
		})
	}

	log := sarifLog{
		Version: "2.1.0",
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "vortex",
						Version:        version.Number,
						InformationURI: "https://github.com/lemon4ksan/aoni",
						Rules:          rules,
					},
				},
				Results: results,
			},
		},
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	return encoder.Encode(log)
}
