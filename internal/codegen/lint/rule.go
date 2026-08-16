// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lint provides a modular contract linter and diagnostic engine for aoni/vortex interfaces.
package lint

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

// Category describes the domain of a linting rule.
type Category string

const (
	// CategoryCorrectness flags contract syntax, type mismatches, and broken invariants.
	CategoryCorrectness Category = "Correctness"

	// CategoryStyle flags non-canonical annotations and naming conventions.
	CategoryStyle Category = "Style"

	// CategoryPerformance flags potential allocation or design inefficiencies.
	CategoryPerformance Category = "Performance"

	// CategoryCodegen flags stale or missing generated files.
	CategoryCodegen Category = "Codegen"
)

// Severity indicates diagnostic impact level.
type Severity string

const (
	// SeverityError halts CI validation and marks contracts as invalid.
	SeverityError Severity = "ERROR"

	// SeverityWarning informs the developer of potential design or efficiency issues.
	SeverityWarning Severity = "WARN"

	// SeverityInfo provides informational suggestions.
	SeverityInfo Severity = "INFO"
)

// Fix represents a safe, non-destructive automated code or artifact modification.
type Fix struct {
	Description string       `json:"description"`
	Apply       func() error `json:"-"`
}

// Diagnostic represents an issue identified by a linting rule.
type Diagnostic struct {
	RuleID     string
	RuleName   string
	Severity   Severity
	Category   Category
	Target     string
	Message    string
	FilePath   string
	Line       int
	Column     int
	Suggestion string
	Fix        *Fix
}

func (d Diagnostic) String() string {
	if d.Line > 0 {
		return fmt.Sprintf("[%s:%s] %s:%d: %s", d.Severity, d.RuleID, d.FilePath, d.Line, d.Message)
	}

	return fmt.Sprintf("[%s:%s] %s: %s", d.Severity, d.RuleID, d.FilePath, d.Message)
}

// Pass encapsulates all AST, IR, and source context passed to a linting rule.
type Pass struct {
	Context     context.Context
	RootIR      *ir.RootIR
	FileSet     *token.FileSet
	ASTFile     *ast.File
	SourceBytes []byte
	FilePath    string
	Ignores     *IgnoreMap
}

// FindNodePosition locates the line and column of a given service or method in the pass AST.
func (p *Pass) FindNodePosition(serviceName, methodName string) (int, int) {
	if p == nil || p.ASTFile == nil || p.FileSet == nil {
		return 1, 1
	}

	for _, decl := range p.ASTFile.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || (serviceName != "" && ts.Name.Name != serviceName) {
				continue
			}

			if methodName == "" {
				pos := p.FileSet.Position(ts.Pos())
				return pos.Line, pos.Column
			}

			iface, ok := ts.Type.(*ast.InterfaceType)
			if !ok || iface.Methods == nil {
				continue
			}

			for _, m := range iface.Methods.List {
				for _, name := range m.Names {
					if name.Name == methodName {
						pos := p.FileSet.Position(name.Pos())
						return pos.Line, pos.Column
					}
				}
			}
		}
	}

	return 1, 1
}

// Rule defines the interface for pluggable contract linters.
type Rule interface {
	// ID returns the unique rule identifier (e.g. "E001", "W001").
	ID() string

	// Name returns the human-readable slug (e.g. "stale-codegen", "param-lifting").
	Name() string

	// Description returns what the rule checks.
	Description() string

	// Category returns the rule domain.
	Category() Category

	// DefaultSeverity returns the default severity.
	DefaultSeverity() Severity

	// IsFixable reports whether the rule provides an automated safe fix.
	IsFixable() bool

	// Run executes the rule against the pass context and returns discovered diagnostics.
	Run(pass *Pass) []Diagnostic
}
