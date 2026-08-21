// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"fmt"

	"github.com/lemon4ksan/foundation/generic"
)

// Severity indicates the critical impact level of an analysis diagnostic.
type Severity string

const (
	// SeverityError indicates a semantic violation that halts code generation.
	SeverityError Severity = "ERROR"

	// SeverityWarning informs the user about non-fatal architectural or RFC design issues.
	SeverityWarning Severity = "WARNING"

	// SeverityInfo provides informative recommendations.
	SeverityInfo Severity = "INFO"
)

// Diagnostic represents a structured semantic finding emitted by an analysis rule.
type Diagnostic struct {
	Code       string   // Unique machine-readable rule code (e.g. "service/duplicate-method")
	Severity   Severity // Error, Warning, or Info
	Target     string   // AST node target identifier (e.g. "UserService.GetUser")
	Message    string   // Human-readable description of the issue
	Suggestion string   // Optional recommendation or typo fix (e.g. "did you mean \"@get\"?")
}

// String formats the diagnostic into a standard compiler diagnostic line.
func (d Diagnostic) String() string {
	if d.Suggestion != "" {
		return fmt.Sprintf("[%s] (%s) %s: %s (suggestion: %s)", d.Severity, d.Code, d.Target, d.Message, d.Suggestion)
	}

	return fmt.Sprintf("[%s] (%s) %s: %s", d.Severity, d.Code, d.Target, d.Message)
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
