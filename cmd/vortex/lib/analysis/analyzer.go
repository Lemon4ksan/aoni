// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"fmt"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// Severity indicates whether a diagnostic is an error or a warning.
type Severity string

const (
	// SeverityError halts code generation.
	SeverityError Severity = "ERROR"

	// SeverityWarning informs the user about non-fatal design issues.
	SeverityWarning Severity = "WARNING"
)

// Diagnostic represents a semantic issue found during IR analysis.
type Diagnostic struct {
	Severity Severity
	Target   string
	Message  string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("[%s] %s: %s", d.Severity, d.Target, d.Message)
}

// HasErrors returns true if the diagnostics list contains at least one SeverityError.
func HasErrors(diags []Diagnostic) bool {
	return generic.Any(diags, func(d Diagnostic) bool {
		return d.Severity == SeverityError
	})
}

// Errors returns only the diagnostics with SeverityError.
func Errors(diags []Diagnostic) []Diagnostic {
	return generic.Filter(diags, func(d Diagnostic) bool {
		return d.Severity == SeverityError
	})
}

// Warnings returns only the diagnostics with SeverityWarning.
func Warnings(diags []Diagnostic) []Diagnostic {
	return generic.Filter(diags, func(d Diagnostic) bool {
		return d.Severity == SeverityWarning
	})
}

// Analyzer checks the semantic validity of a parsed [ir.RootIR].
type Analyzer struct{}

// NewAnalyzer creates a new Analyzer instance.
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Analyze validates the RootIR and returns a slice of diagnostics.
// If any diagnostic has SeverityError, the IR is invalid.
func (a *Analyzer) Analyze(root *ir.RootIR) []Diagnostic {
	var diags []Diagnostic

	if root == nil {
		return []Diagnostic{
			{Severity: SeverityError, Target: "root", Message: "IR root is nil"},
		}
	}

	for _, svc := range root.Services {
		diags = append(diags, analyzeService(svc)...)
	}

	for _, strct := range root.Structs {
		diags = append(diags, validateStructFields(strct)...)
	}

	for _, tuple := range root.Tuples {
		diags = append(diags, validateTupleFields(tuple)...)
	}

	if len(root.UnrecognizedDirectives) > 0 {
		diags = append(diags, validateUnrecognizedDirectives(root.UnrecognizedDirectives)...)
	}

	return diags
}
