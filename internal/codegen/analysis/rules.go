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
	if m.Operation == ir.OpWSOn || m.Operation == ir.OpEvent || m.Operation == ir.OpClose || m.IsEvent {
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

var AllKnownDirectives = []string{
	"aoni:service", "service",
	"aoni:dto", "dto",
	"aoni:tuple", "tuple",
	"aoni:union", "union",
	"aoni:socket", "socket",

	"base_url", "engine", "protocol",
	"requester", "persona", "tls_spec",
	"p0f", "timeout", "retry",
	"circuit", "envelope", "auth",
	"ssh", "ws", "websocket",
	"type_map", "grpc", "op",
	"operation", "notify", "event",
	"call", "get", "post",
	"put", "delete", "patch",
	"head", "options", "return",
	"body", "extract", "codec",
	"header", "form", "multipart",
	"preset", "inject", "referer",
	"unwrap", "query", "field",
	"param", "cookie", "path",
	"part", "file", "check",
	"casing", "format", "idempotent",
	"sign", "coalesce", "etag",
	"cache", "probe", "ratelimit",
	"metric", "stream", "batch",
	"cast", "packet", "opcode",
	"job_id", "endpoint",
	"heartbeat",
}

func levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)

	n, m := len(r1), len(r2)
	if n == 0 {
		return m
	}

	if m == 0 {
		return n
	}

	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
		d[i][0] = i
	}

	for j := 0; j <= m; j++ {
		d[0][j] = j
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}

			del := d[i-1][j] + 1
			ins := d[i][j-1] + 1
			sub := d[i-1][j-1] + cost

			minVal := del
			if ins < minVal {
				minVal = ins
			}

			if sub < minVal {
				minVal = sub
			}

			d[i][j] = minVal
		}
	}

	return d[n][m]
}

// FindClosestDirective returns the closest valid directive name if within edit distance 3.
func FindClosestDirective(input string) string {
	bestMatch := ""
	bestDist := 999

	inputLower := strings.ToLower(input)
	for _, candidate := range AllKnownDirectives {
		dist := levenshteinDistance(inputLower, candidate)
		if dist < bestDist {
			bestDist = dist
			bestMatch = candidate
		}
	}

	if bestDist <= 3 {
		return bestMatch
	}

	return ""
}

func validateUnrecognizedDirectives(uds []ir.UnrecognizedDirectiveIR) []Diagnostic {
	diags := make([]Diagnostic, 0, len(uds))
	for _, ud := range uds {
		msg := fmt.Sprintf("unrecognized directive \"@%s\"", ud.Name)
		if suggestion := FindClosestDirective(ud.Name); suggestion != "" {
			msg = fmt.Sprintf("unrecognized directive \"@%s\" (did you mean \"@%s\"?)", ud.Name, suggestion)
		}

		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Target:   ud.Target,
			Message:  msg,
		})
	}

	return diags
}
