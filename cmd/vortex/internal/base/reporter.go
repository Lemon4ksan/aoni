// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package base

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/lemon4ksan/aoni/internal/codegen/diff"
	"github.com/lemon4ksan/aoni/internal/codegen/lint"
)

// Reporter provides unified output formatting for diffs, diagnostics, and codegen summaries.
type Reporter struct {
	Stdout io.Writer
	Stderr io.Writer
}

// NewReporter creates a new reporter instance writing to stdout and stderr.
func NewReporter(stdout, stderr io.Writer) *Reporter {
	return &Reporter{
		Stdout: stdout,
		Stderr: stderr,
	}
}

// RenderJSON serializes and prints data as indented JSON.
func (r *Reporter) RenderJSON(data any) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("rendering JSON: %w", err)
	}

	fmt.Fprintln(r.Stdout, string(bytes))

	return nil
}

// RenderDiff formats and outputs a semantic contract drift report.
func (r *Reporter) RenderDiff(report *diff.DiffReport, asJSON bool) error {
	if report == nil {
		return nil
	}

	if asJSON {
		jsonBytes, err := report.RenderJSON()
		if err != nil {
			return fmt.Errorf("formatting JSON diff report: %w", err)
		}

		fmt.Fprintln(r.Stdout, string(jsonBytes))

		return nil
	}

	fmt.Fprint(r.Stdout, report.Render(true))

	return nil
}

// RenderDiagnostics renders linter issues across supported formats (terminal, json, github, sarif).
func (r *Reporter) RenderDiagnostics(diags []lint.Diagnostic, format string) error {
	switch format {
	case "json":
		return r.RenderJSON(diags)
	case "github":
		for _, d := range diags {
			level := "warning"
			if d.Severity == lint.SeverityError {
				level = "error"
			}

			fmt.Fprintf(r.Stdout, "::%s file=%s,line=%d::[%s] %s\n", level, d.FilePath, d.Line, d.RuleID, d.Message)
		}

		return nil

	default:
		// Terminal rendering
		if len(diags) == 0 {
			fmt.Fprintln(r.Stdout, "✔ All contracts passed static validation with 0 issues.")
			return nil
		}

		for _, d := range diags {
			fmt.Fprintf(
				r.Stdout,
				"  ↳ [%s:%s] %s:%d:%d\n    %s\n",
				d.RuleID,
				d.RuleName,
				d.FilePath,
				d.Line,
				d.Column,
				d.Message,
			)

			if d.Suggestion != "" {
				fmt.Fprintf(r.Stdout, "    ↳ Suggestion: %s\n", d.Suggestion)
			}
		}

		return nil
	}
}
