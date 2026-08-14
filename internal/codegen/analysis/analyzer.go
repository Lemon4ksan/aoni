// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
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
		diags = append(diags, a.analyzeService(svc)...)
	}

	for _, strct := range root.Structs {
		diags = append(diags, validateStructFields(strct)...)
	}

	for _, tuple := range root.Tuples {
		diags = append(diags, validateTupleFields(tuple)...)
	}

	return diags
}

// HasErrors returns true if the diagnostics list contains at least one SeverityError.
func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}

	return false
}

func (a *Analyzer) analyzeService(svc *ir.ServiceIR) []Diagnostic {
	var diags []Diagnostic

	if svc.Name == "" {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Target:   "service",
			Message:  "service name cannot be empty",
		})
	}

	if len(svc.Methods) == 0 {
		diags = append(diags, Diagnostic{
			Severity: SeverityWarning,
			Target:   svc.Name,
			Message:  "service interface has no declared methods",
		})
	}

	for _, m := range svc.Methods {
		diags = append(diags, a.analyzeMethod(svc.Name, m)...)
	}

	return diags
}

func (a *Analyzer) analyzeMethod(svcName string, m *ir.MethodIR) []Diagnostic {
	var diags []Diagnostic

	target := fmt.Sprintf("%s.%s", svcName, m.Name)

	if d := validateHTTPMethod(target, m); d != nil {
		diags = append(diags, *d)
	}

	if d := validateContextParameter(target, m); d != nil {
		diags = append(diags, *d)
	}

	paramNames := collectParamNames(m.Params)
	diags = append(diags, validatePathVariables(target, m, paramNames)...)
	diags = append(diags, validateDynamicHeaders(target, m, paramNames)...)

	if d := validateReturnSignature(target, m); d != nil {
		diags = append(diags, *d)
	}

	if d := validateBodyPayloadLimit(target, m); d != nil {
		diags = append(diags, *d)
	}

	return diags
}

func collectParamNames(params []*ir.ParamIR) map[string]bool {
	paramNames := make(map[string]bool, len(params)*2)
	for _, p := range params {
		paramNames[p.GoName] = true

		paramNames[strings.ToLower(p.GoName)] = true
		if p.WireKey != "" {
			paramNames[p.WireKey] = true
			paramNames[strings.ToLower(p.WireKey)] = true
		}
	}

	return paramNames
}
