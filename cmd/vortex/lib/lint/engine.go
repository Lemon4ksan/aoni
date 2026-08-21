// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"errors"
	"fmt"
)

// Report aggregates discovered diagnostics and check statistics.
type Report struct {
	Diagnostics     []Diagnostic
	SuppressedCount int
	ServicesChecked int
	MethodsChecked  int
	FilesChecked    int
}

// Errors returns the count of SeverityError diagnostics.
func (r *Report) Errors() int {
	var count int
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			count++
		}
	}

	return count
}

// Warnings returns the count of SeverityWarning diagnostics.
func (r *Report) Warnings() int {
	var count int
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityWarning {
			count++
		}
	}

	return count
}

// FixableCount returns the count of diagnostics with automated safe fixes.
func (r *Report) FixableCount() int {
	var count int
	for _, d := range r.Diagnostics {
		if d.Fix != nil {
			count++
		}
	}

	return count
}

// ApplyFixes executes all safe automated fixes and returns the number of applied fixes.
func (r *Report) ApplyFixes() (int, error) {
	var applied int
	for _, d := range r.Diagnostics {
		if d.Fix != nil && d.Fix.Apply != nil {
			if err := d.Fix.Apply(); err != nil {
				return applied, fmt.Errorf("apply fix for [%s] %s: %w", d.RuleID, d.Message, err)
			}

			applied++
		}
	}

	return applied, nil
}

// Engine coordinates the execution of linting passes against parsed contracts.
type Engine struct {
	registry *Registry
}

// NewEngine constructs an Engine with the given rule registry.
func NewEngine(reg *Registry) *Engine {
	if reg == nil {
		reg = DefaultRegistry()
	}

	return &Engine{registry: reg}
}

// Run executes all active rules on the provided pass context.
func (e *Engine) Run(pass *Pass) (*Report, error) {
	if pass == nil {
		return nil, errors.New("lint: pass context cannot be nil")
	}

	report := &Report{
		FilesChecked: 1,
	}

	if pass.RootIR != nil {
		report.ServicesChecked = len(pass.RootIR.Services)
		for _, s := range pass.RootIR.Services {
			report.MethodsChecked += len(s.Methods)
		}
	}

	// Parse suppressions if AST is provided and ignores not already populated
	if pass.Ignores == nil && pass.ASTFile != nil && pass.FileSet != nil {
		pass.Ignores = ParseIgnores(pass.FileSet, pass.ASTFile)
	}

	activeRules := e.registry.ActiveRules()
	for _, rule := range activeRules {
		diags := rule.Run(pass)
		for _, d := range diags {
			// Check suppression
			if pass.Ignores != nil && pass.Ignores.IsIgnored(d.RuleID, d.RuleName, d.Target, d.Line) {
				report.SuppressedCount++
				continue
			}

			report.Diagnostics = append(report.Diagnostics, d)
		}
	}

	return report, nil
}

// MergeReports combines multiple Reports into a single comprehensive summary.
func MergeReports(reports ...*Report) *Report {
	merged := &Report{}
	for _, r := range reports {
		if r == nil {
			continue
		}

		merged.Diagnostics = append(merged.Diagnostics, r.Diagnostics...)
		merged.SuppressedCount += r.SuppressedCount
		merged.ServicesChecked += r.ServicesChecked
		merged.MethodsChecked += r.MethodsChecked
		merged.FilesChecked += r.FilesChecked
	}

	return merged
}
