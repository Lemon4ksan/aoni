// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func validateHTTPMethod(target string, m *ir.MethodIR) *Diagnostic {
	if m.Operation == ir.OpHTTP && m.HTTPMethod == "" {
		return &Diagnostic{
			Severity: SeverityError,
			Target:   target,
			Message:  "missing HTTP method directive (e.g. @get, @post, @put, @delete)",
		}
	}

	return nil
}

func validateContextParameter(target string, m *ir.MethodIR) *Diagnostic {
	if m.Operation == ir.OpWSOn {
		return nil
	}

	if len(m.Params) == 0 || m.Params[0].Location != ir.LocContext {
		return &Diagnostic{
			Severity: SeverityError,
			Target:   target,
			Message:  "first method parameter must be context.Context",
		}
	}

	return nil
}

func validatePathVariables(target string, m *ir.MethodIR, paramNames map[string]bool) []Diagnostic {
	if m.Path == nil {
		return nil
	}

	var diags []Diagnostic

	for _, seg := range m.Path.Segments {
		if seg.IsVariable {
			varName := seg.VarName
			if !paramNames[varName] && !paramNames[strings.ToLower(varName)] {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Target:   target,
					Message:  fmt.Sprintf("path variable {%s} does not match any method parameter", varName),
				})
			}
		}
	}

	return diags
}

func validateDynamicHeaders(target string, m *ir.MethodIR, paramNames map[string]bool) []Diagnostic {
	var diags []Diagnostic

	for _, h := range m.Headers {
		if h.DynamicTemplate == nil {
			continue
		}

		for _, seg := range h.DynamicTemplate.Segments {
			if !seg.IsVariable {
				continue
			}

			varName := seg.VarName

			rootVar := strings.Split(varName, ".")[0]
			if paramNames[varName] || paramNames[strings.ToLower(varName)] ||
				paramNames[rootVar] || paramNames[strings.ToLower(rootVar)] {
				continue
			}

			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Target:   target,
				Message: fmt.Sprintf(
					"dynamic header {%s} variable does not match any method parameter",
					varName,
				),
			})
		}
	}

	return diags
}

func validateReturnSignature(target string, m *ir.MethodIR) *Diagnostic {
	if m.Return == nil {
		return &Diagnostic{
			Severity: SeverityError,
			Target:   target,
			Message:  "method return signature cannot be empty (must return at least error)",
		}
	}

	return nil
}

func validateBodyPayloadLimit(target string, m *ir.MethodIR) *Diagnostic {
	bodyCount := 0
	for _, p := range m.Params {
		if p.Location == ir.LocBody {
			bodyCount++
		}
	}

	if bodyCount > 1 {
		return &Diagnostic{
			Severity: SeverityError,
			Target:   target,
			Message:  fmt.Sprintf("method has %d request body parameters; at most 1 is allowed", bodyCount),
		}
	}

	return nil
}

func validateStructFields(s *ir.StructIR) []Diagnostic {
	var diags []Diagnostic

	wireNames := make(map[string]string)
	for _, f := range s.Fields {
		if f.WireName == "" {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Target:   fmt.Sprintf("%s.%s", s.Name, f.GoName),
				Message:  "wire field name cannot be empty",
			})

			continue
		}

		if prevGoName, exists := wireNames[f.WireName]; exists {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Target:   fmt.Sprintf("%s.%s", s.Name, f.GoName),
				Message:  fmt.Sprintf("duplicate wire name %q (conflicts with %s)", f.WireName, prevGoName),
			})

			continue
		}

		wireNames[f.WireName] = f.GoName
	}

	return diags
}

func validateTupleFields(t *ir.TupleIR) []Diagnostic {
	if len(t.Fields) == 0 {
		return []Diagnostic{
			{
				Severity: SeverityError,
				Target:   t.Name,
				Message:  "tuple must contain at least one field",
			},
		}
	}

	return nil
}
