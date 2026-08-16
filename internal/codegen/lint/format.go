// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
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

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].count != stats[j].count {
			return stats[i].count > stats[j].count
		}

		return stats[i].ruleKey < stats[j].ruleKey
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
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].RuleID != diags[j].RuleID {
			return diags[i].RuleID < diags[j].RuleID
		}

		if diags[i].FilePath != diags[j].FilePath {
			return diags[i].FilePath < diags[j].FilePath
		}

		if diags[i].Line != diags[j].Line {
			return diags[i].Line < diags[j].Line
		}

		return diags[i].Column < diags[j].Column
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
