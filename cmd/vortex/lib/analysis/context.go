// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"fmt"
	"strings"
)

// Context encapsulates diagnostic reporting state during an analysis pipeline execution pass.
type Context struct {
	diags []Diagnostic
}

// NewContext creates a new initialized [Context].
func NewContext() *Context {
	return &Context{
		diags: make([]Diagnostic, 0, 16),
	}
}

// Error records a [SeverityError] diagnostic.
func (c *Context) Error(code, target, message string) {
	c.diags = append(c.diags, Diagnostic{
		Code:     code,
		Severity: SeverityError,
		Target:   target,
		Message:  message,
	})
}

// Errorf records a formatted [SeverityError] diagnostic.
func (c *Context) Errorf(code, target, format string, args ...any) {
	c.Error(code, target, fmt.Sprintf(format, args...))
}

// Warn records a [SeverityWarning] diagnostic.
func (c *Context) Warn(code, target, message string) {
	c.diags = append(c.diags, Diagnostic{
		Code:     code,
		Severity: SeverityWarning,
		Target:   target,
		Message:  message,
	})
}

// Warnf records a formatted [SeverityWarning] diagnostic.
func (c *Context) Warnf(code, target, format string, args ...any) {
	c.Warn(code, target, fmt.Sprintf(format, args...))
}

// ReportWithSuggestion records a diagnostic with an actionable fix suggestion.
func (c *Context) ReportWithSuggestion(code string, severity Severity, target, message, suggestion string) {
	c.diags = append(c.diags, Diagnostic{
		Code:       code,
		Severity:   severity,
		Target:     target,
		Message:    message,
		Suggestion: suggestion,
	})
}

// Target formats path or symbol identifiers into dot-separated paths (e.g. "Service.Method").
func (c *Context) Target(parts ...string) string {
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}

		if i > 0 && b.Len() > 0 {
			b.WriteByte('.')
		}

		b.WriteString(p)
	}

	return b.String()
}

// Diagnostics returns all recorded findings.
func (c *Context) Diagnostics() []Diagnostic {
	return c.diags
}
