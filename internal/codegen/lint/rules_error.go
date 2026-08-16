// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/mirror"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// RuleStaleCodegen checks if generated *.gen.go files are missing or out of sync.
type RuleStaleCodegen struct{}

func (r *RuleStaleCodegen) ID() string   { return "E001" }
func (r *RuleStaleCodegen) Name() string { return "stale-codegen" }
func (r *RuleStaleCodegen) Description() string {
	return "Checks if target *.gen.go files are missing or out of date"
}
func (r *RuleStaleCodegen) Category() Category        { return CategoryCodegen }
func (r *RuleStaleCodegen) DefaultSeverity() Severity { return SeverityError }
func (r *RuleStaleCodegen) IsFixable() bool           { return true }

func (r *RuleStaleCodegen) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil || pass.FilePath == "" {
		return nil
	}

	// Calculate expected generated file path: e.g. api.go -> api.gen.go
	genPath := strings.TrimSuffix(pass.FilePath, ".go") + ".gen.go"

	// Optimize and generate expected code in-memory
	optimizer.NewOptimizer().Optimize(pass.RootIR)

	expectedCode, err := emitter.NewEmitter().Emit(pass.RootIR)
	if err != nil {
		return nil
	}

	existingBytes, err := os.ReadFile(genPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Diagnostic{
				{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     pass.FilePath,
					FilePath:   pass.FilePath,
					Line:       1,
					Column:     1,
					Message:    fmt.Sprintf("Generated file %s does not exist", genPath),
					Suggestion: "Run `vortex gen` or `vortex check --fix` to emit generated contract code",
					Fix: &Fix{
						Description: "Generate " + genPath,
						Apply: func() error {
							return os.WriteFile(genPath, expectedCode, 0o600)
						},
					},
				},
			}
		}

		return nil
	}

	if string(existingBytes) != string(expectedCode) {
		return []Diagnostic{
			{
				RuleID:     r.ID(),
				RuleName:   r.Name(),
				Severity:   r.DefaultSeverity(),
				Category:   r.Category(),
				Target:     pass.FilePath,
				FilePath:   pass.FilePath,
				Line:       1,
				Column:     1,
				Message:    fmt.Sprintf("Generated file %s is out of date with interface contract", genPath),
				Suggestion: "Run `vortex check --fix` to synchronize generated artifacts",
				Fix: &Fix{
					Description: "Re-generate " + genPath,
					Apply: func() error {
						return os.WriteFile(genPath, expectedCode, 0o600)
					},
				},
			},
		}
	}

	return nil
}

// RuleUnmatchedPath checks that all path variables {var} in URLs match declared method parameters.
type RuleUnmatchedPath struct{}

func (r *RuleUnmatchedPath) ID() string   { return "E002" }
func (r *RuleUnmatchedPath) Name() string { return "unmatched-path" }
func (r *RuleUnmatchedPath) Description() string {
	return "Checks that path variable placeholders match method parameters"
}
func (r *RuleUnmatchedPath) Category() Category        { return CategoryCorrectness }
func (r *RuleUnmatchedPath) DefaultSeverity() Severity { return SeverityError }
func (r *RuleUnmatchedPath) IsFixable() bool           { return false }

func (r *RuleUnmatchedPath) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			if m.Path == nil {
				continue
			}

			paramNames := make(map[string]bool, len(m.Params)*4)
			for _, p := range m.Params {
				paramNames[p.GoName] = true
				paramNames[strings.ToLower(p.GoName)] = true
				paramNames[p.WireKey] = true
				paramNames[strings.ToLower(p.WireKey)] = true
				paramNames[parser.ToCasing(p.GoName, ir.CasingSnakeCase)] = true
				paramNames[parser.ToCasing(p.GoName, ir.CasingFlatCase)] = true
			}

			target := fmt.Sprintf("%s.%s", svc.Name, m.Name)
			line, col := pass.FindNodePosition(svc.Name, m.Name)

			for _, seg := range m.Path.Segments {
				if seg.IsVariable && !paramNames[seg.VarName] && !paramNames[strings.ToLower(seg.VarName)] {
					diags = append(diags, Diagnostic{
						RuleID:   r.ID(),
						RuleName: r.Name(),
						Severity: r.DefaultSeverity(),
						Category: r.Category(),
						Target:   target,
						FilePath: pass.FilePath,
						Line:     line,
						Column:   col,
						Message: fmt.Sprintf(
							"Path variable {%s} in URL does not match any method parameter",
							seg.VarName,
						),
						Suggestion: fmt.Sprintf(
							"Add parameter `%s` to method `%s` or adjust the path template",
							seg.VarName,
							m.Name,
						),
					})
				}
			}
		}
	}

	return diags
}

// RuleMissingHTTPMethod checks that HTTP methods specify an operation directive (@get, @post, etc.).
type RuleMissingHTTPMethod struct{}

func (r *RuleMissingHTTPMethod) ID() string   { return "E003" }
func (r *RuleMissingHTTPMethod) Name() string { return "missing-http-method" }
func (r *RuleMissingHTTPMethod) Description() string {
	return "Checks that HTTP operation has a method directive (@get, @post, etc.)"
}
func (r *RuleMissingHTTPMethod) Category() Category        { return CategoryCorrectness }
func (r *RuleMissingHTTPMethod) DefaultSeverity() Severity { return SeverityError }
func (r *RuleMissingHTTPMethod) IsFixable() bool           { return false }

func (r *RuleMissingHTTPMethod) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		if svc.Protocol != ir.ProtocolHTTP && svc.Protocol != "" {
			continue
		}

		for _, m := range svc.Methods {
			if m.Operation == ir.OpHTTP && m.HTTPMethod == "" {
				target := fmt.Sprintf("%s.%s", svc.Name, m.Name)
				line, col := pass.FindNodePosition(svc.Name, m.Name)

				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     target,
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    "Missing HTTP method directive (e.g. @get, @post, @put, @delete)",
					Suggestion: "Annotate method with `@get /path` or appropriate HTTP verb",
				})
			}
		}
	}

	return diags
}

// RuleMissingContext checks that the first parameter of an API method is context.Context.
type RuleMissingContext struct{}

func (r *RuleMissingContext) ID() string   { return "E004" }
func (r *RuleMissingContext) Name() string { return "missing-context" }
func (r *RuleMissingContext) Description() string {
	return "Checks that first parameter is context.Context"
}
func (r *RuleMissingContext) Category() Category        { return CategoryCorrectness }
func (r *RuleMissingContext) DefaultSeverity() Severity { return SeverityError }
func (r *RuleMissingContext) IsFixable() bool           { return false }

func (r *RuleMissingContext) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		if svc.Protocol != ir.ProtocolHTTP && svc.Protocol != "" {
			continue
		}

		for _, m := range svc.Methods {
			if m.Operation == ir.OpWSOn || m.Operation == ir.OpEvent || m.Operation == ir.OpClose || m.IsEvent {
				continue
			}

			if len(m.Params) == 0 || m.Params[0].Location != ir.LocContext {
				target := fmt.Sprintf("%s.%s", svc.Name, m.Name)
				line, col := pass.FindNodePosition(svc.Name, m.Name)

				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     target,
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    "First method parameter must be context.Context",
					Suggestion: "Add `ctx context.Context` as the first argument in method signature",
				})
			}
		}
	}

	return diags
}

// RuleUnrecognizedDirective checks for unrecognized @aoni directives.
type RuleUnrecognizedDirective struct{}

func (r *RuleUnrecognizedDirective) ID() string   { return "E005" }
func (r *RuleUnrecognizedDirective) Name() string { return "unrecognized-directive" }
func (r *RuleUnrecognizedDirective) Description() string {
	return "Checks for unrecognized or misspelled @aoni directives"
}
func (r *RuleUnrecognizedDirective) Category() Category        { return CategoryCorrectness }
func (r *RuleUnrecognizedDirective) DefaultSeverity() Severity { return SeverityError }
func (r *RuleUnrecognizedDirective) IsFixable() bool           { return false }

func (r *RuleUnrecognizedDirective) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	src := string(pass.SourceBytes)
	lines := strings.Split(src, "\n")

	var diags []Diagnostic
	for _, raw := range pass.RootIR.UnrecognizedDirectives {
		target := raw.Target
		if target == "" {
			target = pass.FilePath
		}

		line := 1
		col := 1
		pattern := "@" + raw.Name

		for i, l := range lines {
			if idx := strings.Index(l, pattern); idx >= 0 {
				line = i + 1
				col = idx + 1

				break
			}
		}

		diags = append(diags, Diagnostic{
			RuleID:     r.ID(),
			RuleName:   r.Name(),
			Severity:   r.DefaultSeverity(),
			Category:   r.Category(),
			Target:     target,
			FilePath:   pass.FilePath,
			Line:       line,
			Column:     col,
			Message:    fmt.Sprintf("Unrecognized directive %q", raw.Name),
			Suggestion: "Check spelling or run `vortex list` to see all supported directives",
		})
	}

	return diags
}

// RuleConflictingPayload checks if a method specifies mutually exclusive payload encodings.
type RuleConflictingPayload struct{}

func (r *RuleConflictingPayload) ID() string   { return "E006" }
func (r *RuleConflictingPayload) Name() string { return "conflicting-payload" }
func (r *RuleConflictingPayload) Description() string {
	return "Detects conflicting or mutually exclusive request payload directives"
}
func (r *RuleConflictingPayload) Category() Category        { return CategoryCorrectness }
func (r *RuleConflictingPayload) DefaultSeverity() Severity { return SeverityError }
func (r *RuleConflictingPayload) IsFixable() bool           { return false }

func (r *RuleConflictingPayload) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			bodyLocations := make(map[ir.ParamLocation]bool)
			for _, p := range m.Params {
				switch p.Location {
				case ir.LocBody, ir.LocFormFields, ir.LocMultipartField, ir.LocMultipartFile:
					bodyLocations[p.Location] = true
				}
			}

			if (bodyLocations[ir.LocBody] && (bodyLocations[ir.LocFormFields] || bodyLocations[ir.LocMultipartField])) ||
				(bodyLocations[ir.LocFormFields] && bodyLocations[ir.LocMultipartField]) {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     fmt.Sprintf("%s.%s", svc.Name, m.Name),
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    fmt.Sprintf("Method %s has conflicting body payload encodings", m.Name),
					Suggestion: "Choose exactly one body payload format (@body, @form, or @multipart)",
				})
			}
		}
	}

	return diags
}

// RuleIllegalBodyMethod checks that GET and HEAD methods do not have request bodies.
type RuleIllegalBodyMethod struct{}

func (r *RuleIllegalBodyMethod) ID() string   { return "E007" }
func (r *RuleIllegalBodyMethod) Name() string { return "illegal-body-method" }
func (r *RuleIllegalBodyMethod) Description() string {
	return "Enforces RFC 9110 prohibition of request bodies on GET and HEAD methods"
}
func (r *RuleIllegalBodyMethod) Category() Category        { return CategoryCorrectness }
func (r *RuleIllegalBodyMethod) DefaultSeverity() Severity { return SeverityError }
func (r *RuleIllegalBodyMethod) IsFixable() bool           { return false }

func (r *RuleIllegalBodyMethod) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			isGetOrHead := strings.EqualFold(m.HTTPMethod, http.MethodGet) ||
				strings.EqualFold(m.HTTPMethod, http.MethodHead)
			if !isGetOrHead {
				continue
			}

			hasBody := false
			for _, p := range m.Params {
				switch p.Location {
				case ir.LocBody, ir.LocFormFields, ir.LocMultipartField, ir.LocMultipartFile:
					hasBody = true
				}
			}

			if hasBody {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
					FilePath: pass.FilePath,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"HTTP %s method %s cannot have a request body payload (RFC 9110 violation)",
						m.HTTPMethod,
						m.Name,
					),
					Suggestion: "Change HTTP verb to @post/@put or send parameters as @query",
				})
			}
		}
	}

	return diags
}

// RuleMissingErrorReturn enforces that all network interface methods return error.
type RuleMissingErrorReturn struct{}

func (r *RuleMissingErrorReturn) ID() string   { return "E008" }
func (r *RuleMissingErrorReturn) Name() string { return "missing-error-return" }
func (r *RuleMissingErrorReturn) Description() string {
	return "Enforces that all network methods return error as their final return value"
}
func (r *RuleMissingErrorReturn) Category() Category        { return CategoryCorrectness }
func (r *RuleMissingErrorReturn) DefaultSeverity() Severity { return SeverityError }
func (r *RuleMissingErrorReturn) IsFixable() bool           { return false }

func (r *RuleMissingErrorReturn) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			if m.IsEvent || m.Operation == ir.OpEvent || m.IsNotify || m.Operation == ir.OpNotify {
				continue
			}

			if svc.Protocol == ir.ProtocolSocket {
				if m.Name == "IsConnected" || strings.HasPrefix(m.Name, "Register") ||
					m.Name == "Connector" || m.Name == "Dispatcher" {
					continue
				}
			}

			if (svc.Protocol == ir.ProtocolRPC || svc.Protocol == ir.ProtocolChannel) && m.Name == "Close" {
				continue
			}

			hasErrorReturn := false
			if m.Return != nil && m.Return.SuccessType.Name != "" {
				hasErrorReturn = true
			}

			if !hasErrorReturn {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     fmt.Sprintf("%s.%s", svc.Name, m.Name),
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    fmt.Sprintf("Method %s must return 'error' as its final return value", m.Name),
					Suggestion: "Update signature to return (*Response, error) or error",
				})
			}
		}
	}

	return diags
}

// RuleUnboundHeaderVariable checks if a templated header contains variables not found in parameters or injections.
type RuleUnboundHeaderVariable struct{}

func (r *RuleUnboundHeaderVariable) ID() string   { return "E009" }
func (r *RuleUnboundHeaderVariable) Name() string { return "unbound-header-variable" }
func (r *RuleUnboundHeaderVariable) Description() string {
	return "Detects unbound template variables in request headers"
}
func (r *RuleUnboundHeaderVariable) Category() Category        { return CategoryCorrectness }
func (r *RuleUnboundHeaderVariable) DefaultSeverity() Severity { return SeverityError }
func (r *RuleUnboundHeaderVariable) IsFixable() bool           { return false }

func (r *RuleUnboundHeaderVariable) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			paramSet := make(map[string]bool)
			for _, p := range m.Params {
				paramSet[p.GoName] = true
				paramSet[p.WireKey] = true
			}

			for _, inj := range m.Injects {
				paramSet[inj.WireKey] = true
			}

			for _, h := range m.Headers {
				val := h.StaticValue
				if h.DynamicTemplate != nil {
					val = h.DynamicTemplate.RawTemplate
				}

				start := 0
				for {
					openIdx := strings.Index(val[start:], "{")
					if openIdx == -1 {
						break
					}

					openIdx += start

					closeIdx := strings.Index(val[openIdx:], "}")
					if closeIdx == -1 {
						break
					}

					closeIdx += openIdx

					varName := val[openIdx+1 : closeIdx]
					varName, _, _ = strings.Cut(varName, ":") // strip filters like {id:path_escape}

					if !paramSet[varName] {
						line, col := pass.FindNodePosition(svc.Name, m.Name)
						diags = append(diags, Diagnostic{
							RuleID:   r.ID(),
							RuleName: r.Name(),
							Severity: r.DefaultSeverity(),
							Category: r.Category(),
							Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
							FilePath: pass.FilePath,
							Line:     line,
							Column:   col,
							Message: fmt.Sprintf(
								"Header '%s' contains unbound template variable '{%s}'",
								h.Key,
								varName,
							),
							Suggestion: fmt.Sprintf(
								"Add parameter '%s' to method %s signature or bind via @inject",
								varName,
								m.Name,
							),
						})
					}

					start = closeIdx + 1
				}
			}
		}
	}

	return diags
}

// RuleDuplicateRouteCollision detects two methods in the same service with identical HTTP verb and path route.
type RuleDuplicateRouteCollision struct{}

func (r *RuleDuplicateRouteCollision) ID() string   { return "E010" }
func (r *RuleDuplicateRouteCollision) Name() string { return "duplicate-route-collision" }
func (r *RuleDuplicateRouteCollision) Description() string {
	return "Detects duplicate route collisions in service contracts"
}
func (r *RuleDuplicateRouteCollision) Category() Category        { return CategoryCorrectness }
func (r *RuleDuplicateRouteCollision) DefaultSeverity() Severity { return SeverityError }
func (r *RuleDuplicateRouteCollision) IsFixable() bool           { return false }

func (r *RuleDuplicateRouteCollision) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		seen := make(map[string]string) // "GET /path" -> methodName

		for _, m := range svc.Methods {
			if m.Path == nil || m.HTTPMethod == "" {
				continue
			}

			key := fmt.Sprintf("%s %s", m.HTTPMethod, m.Path.RawTemplate)
			if prevMethod, ok := seen[key]; ok {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
					FilePath: pass.FilePath,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"Duplicate route collision: '%s' is already defined by method %s",
						key,
						prevMethod,
					),
					Suggestion: "Differentiate path routes or combine methods",
				})
			} else {
				seen[key] = m.Name
			}
		}
	}

	return diags
}

// RuleInvalidCheckField checks if @check directive targets a nonexistent field on the response struct.
type RuleInvalidCheckField struct{}

func (r *RuleInvalidCheckField) ID() string   { return "E011" }
func (r *RuleInvalidCheckField) Name() string { return "invalid-check-field" }
func (r *RuleInvalidCheckField) Description() string {
	return "Validates that @check field assertions match fields on the response model"
}
func (r *RuleInvalidCheckField) Category() Category        { return CategoryCorrectness }
func (r *RuleInvalidCheckField) DefaultSeverity() Severity { return SeverityError }
func (r *RuleInvalidCheckField) IsFixable() bool           { return false }

func (r *RuleInvalidCheckField) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	structMap := make(map[string]*ir.StructIR)
	for _, s := range pass.RootIR.Structs {
		structMap[s.Name] = s
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			if len(m.Checks) == 0 || m.Return == nil || m.Return.SuccessType.Name == "" {
				continue
			}

			typeName := m.Return.SuccessType.Name

			strct, ok := structMap[typeName]
			if !ok {
				continue
			}

			fieldSet := make(map[string]bool)
			for _, f := range strct.Fields {
				fieldSet[f.GoName] = true
			}

			for _, chk := range m.Checks {
				if !fieldSet[chk.Field] {
					line, col := pass.FindNodePosition(svc.Name, m.Name)
					diags = append(diags, Diagnostic{
						RuleID:   r.ID(),
						RuleName: r.Name(),
						Severity: r.DefaultSeverity(),
						Category: r.Category(),
						Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
						FilePath: pass.FilePath,
						Line:     line,
						Column:   col,
						Message: fmt.Sprintf(
							"Field '%s' in @check directive does not exist on response struct %s",
							chk.Field,
							typeName,
						),
						Suggestion: "Verify field name spelling in struct " + typeName,
					})
				}
			}
		}
	}

	return diags
}

// RuleInvalidBitpack checks that @aoni:bitpack struct fields have valid bit widths and don't overflow their underlying types.
type RuleInvalidBitpack struct{}

func (r *RuleInvalidBitpack) ID() string   { return "E012" }
func (r *RuleInvalidBitpack) Name() string { return "invalid-bitpack" }
func (r *RuleInvalidBitpack) Description() string {
	return "Validates @aoni:bitpack field bit widths and type safety"
}
func (r *RuleInvalidBitpack) Category() Category        { return CategoryCorrectness }
func (r *RuleInvalidBitpack) DefaultSeverity() Severity { return SeverityError }
func (r *RuleInvalidBitpack) IsFixable() bool           { return false }

func (r *RuleInvalidBitpack) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, bp := range pass.RootIR.Bitpacks {
		line, col := pass.FindNodePosition(bp.Name, "")

		if len(bp.Fields) == 0 || bp.TotalBits == 0 {
			diags = append(diags, Diagnostic{
				RuleID:     r.ID(),
				RuleName:   r.Name(),
				Severity:   r.DefaultSeverity(),
				Category:   r.Category(),
				Target:     bp.Name,
				FilePath:   pass.FilePath,
				Line:       line,
				Column:     col,
				Message:    fmt.Sprintf("Bitpack struct %s has 0 bitfields", bp.Name),
				Suggestion: "Declare struct fields with `bits:\"<N>\"` tags",
			})

			continue
		}

		for _, f := range bp.Fields {
			target := fmt.Sprintf("%s.%s", bp.Name, f.GoName)

			switch {
			case f.BitWidth <= 0:
				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     target,
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    fmt.Sprintf("Field %s has invalid bit width %d (must be > 0)", f.GoName, f.BitWidth),
					Suggestion: "Specify positive bit width, e.g. `bits:\"8\"`",
				})

			case f.IsBool && f.BitWidth != 1:
				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     target,
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    fmt.Sprintf("Bool field %s must have bit width 1 (got %d)", f.GoName, f.BitWidth),
					Suggestion: "Use `bits:\"1\"` for boolean fields",
				})

			case !f.IsBool:
				maxBits := defaultBitWidth(f.Type.Name)
				if f.BitWidth > maxBits {
					diags = append(diags, Diagnostic{
						RuleID:   r.ID(),
						RuleName: r.Name(),
						Severity: r.DefaultSeverity(),
						Category: r.Category(),
						Target:   target,
						FilePath: pass.FilePath,
						Line:     line,
						Column:   col,
						Message: fmt.Sprintf(
							"Field %s with type %s exceeds maximum type bit width %d (got %d bits)",
							f.GoName,
							f.Type.Name,
							maxBits,
							f.BitWidth,
						),
						Suggestion: fmt.Sprintf("Widen underlying Go type or reduce bit width to <= %d", maxBits),
					})
				}
			}
		}
	}

	return diags
}

// RuleShadowedWireName detects two parameters in the same method that serialize to the same wire key.
type RuleShadowedWireName struct{}

func (r *RuleShadowedWireName) ID() string   { return "E013" }
func (r *RuleShadowedWireName) Name() string { return "shadowed-wire-name" }
func (r *RuleShadowedWireName) Description() string {
	return "Detects parameter wire key collisions in query and form payloads"
}
func (r *RuleShadowedWireName) Category() Category        { return CategoryCorrectness }
func (r *RuleShadowedWireName) DefaultSeverity() Severity { return SeverityError }
func (r *RuleShadowedWireName) IsFixable() bool           { return false }

func (r *RuleShadowedWireName) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			seenQuery := make(map[string]string)
			seenForm := make(map[string]string)

			for _, p := range m.Params {
				if p.WireKey == "" {
					continue
				}

				switch p.Location {
				case ir.LocQuery:
					if prev, ok := seenQuery[p.WireKey]; ok {
						line, col := pass.FindNodePosition(svc.Name, m.Name)
						diags = append(diags, Diagnostic{
							RuleID:   r.ID(),
							RuleName: r.Name(),
							Severity: r.DefaultSeverity(),
							Category: r.Category(),
							Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
							FilePath: pass.FilePath,
							Line:     line,
							Column:   col,
							Message: fmt.Sprintf(
								"Wire key collision: query parameters '%s' and '%s' both serialize to '%s'",
								prev,
								p.GoName,
								p.WireKey,
							),
							Suggestion: "Rename one of the parameters or specify explicit @query tags",
						})
					} else {
						seenQuery[p.WireKey] = p.GoName
					}

				case ir.LocFormFields:
					if prev, ok := seenForm[p.WireKey]; ok {
						line, col := pass.FindNodePosition(svc.Name, m.Name)
						diags = append(diags, Diagnostic{
							RuleID:   r.ID(),
							RuleName: r.Name(),
							Severity: r.DefaultSeverity(),
							Category: r.Category(),
							Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
							FilePath: pass.FilePath,
							Line:     line,
							Column:   col,
							Message: fmt.Sprintf(
								"Wire key collision: form parameters '%s' and '%s' both serialize to '%s'",
								prev,
								p.GoName,
								p.WireKey,
							),
							Suggestion: "Rename one of the parameters or specify explicit @form tags",
						})
					} else {
						seenForm[p.WireKey] = p.GoName
					}
				}
			}
		}
	}

	return diags
}

// RuleInvalidUnionStatus validates that @aoni:union structs define valid variant fields and status tags.
type RuleInvalidUnionStatus struct{}

func (r *RuleInvalidUnionStatus) ID() string   { return "E014" }
func (r *RuleInvalidUnionStatus) Name() string { return "invalid-union-status" }
func (r *RuleInvalidUnionStatus) Description() string {
	return "Validates @aoni:union struct definitions and status code mappings"
}
func (r *RuleInvalidUnionStatus) Category() Category        { return CategoryCorrectness }
func (r *RuleInvalidUnionStatus) DefaultSeverity() Severity { return SeverityError }
func (r *RuleInvalidUnionStatus) IsFixable() bool           { return false }

func (r *RuleInvalidUnionStatus) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, u := range pass.RootIR.Unions {
		line, col := pass.FindNodePosition(u.Name, "")

		if len(u.Fields) == 0 {
			diags = append(diags, Diagnostic{
				RuleID:     r.ID(),
				RuleName:   r.Name(),
				Severity:   r.DefaultSeverity(),
				Category:   r.Category(),
				Target:     u.Name,
				FilePath:   pass.FilePath,
				Line:       line,
				Column:     col,
				Message:    fmt.Sprintf("Union struct %s has no variant fields", u.Name),
				Suggestion: "Declare variant fields with `status:\"200,201\"` struct tags",
			})

			continue
		}

		for _, f := range u.Fields {
			if len(f.StatusCodes) == 0 {
				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     fmt.Sprintf("%s.%s", u.Name, f.GoName),
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    fmt.Sprintf("Variant field %s.%s is missing `status:\"...\"` tag", u.Name, f.GoName),
					Suggestion: "Add `status:\"200\"` (or matching HTTP codes) to the field tag",
				})
			}
		}
	}

	return diags
}

func defaultBitWidth(typeName string) int {
	switch typeName {
	case "bool":
		return 1
	case "uint8", "byte", "int8":
		return 8
	case "uint16", "int16":
		return 16
	case "uint32", "int32":
		return 32
	case "uint64", "int64", "uint", "int", "uintptr":
		return 64
	default:
		return 64
	}
}

// RuleMirrorSourceNotFound checks if the @mirror root source file or target interface exists.
type RuleMirrorSourceNotFound struct{}

func (r *RuleMirrorSourceNotFound) ID() string   { return "E015" }
func (r *RuleMirrorSourceNotFound) Name() string { return "mirror-source-not-found" }
func (r *RuleMirrorSourceNotFound) Description() string {
	return "Checks if target Go source file or interface specified in @mirror exists on disk"
}
func (r *RuleMirrorSourceNotFound) Category() Category        { return CategoryCorrectness }
func (r *RuleMirrorSourceNotFound) DefaultSeverity() Severity { return SeverityError }
func (r *RuleMirrorSourceNotFound) IsFixable() bool           { return false }

func (r *RuleMirrorSourceNotFound) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil || pass.FilePath == "" {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		if svc.Mirror == nil || svc.Mirror.Source == "" {
			continue
		}

		line, col := pass.FindNodePosition(svc.Name, "")

		driftDiags, err := mirror.CheckService(pass.RootDir, pass.FilePath, svc, pass.RootIR.Structs)
		if err != nil {
			diags = append(diags, Diagnostic{
				RuleID:     r.ID(),
				RuleName:   r.Name(),
				Severity:   r.DefaultSeverity(),
				Category:   r.Category(),
				Target:     svc.Name,
				FilePath:   pass.FilePath,
				Line:       line,
				Column:     col,
				Message:    err.Error(),
				Suggestion: "Ensure the path in @mirror points to an existing Go source file and valid interface",
			})

			continue
		}

		for _, d := range driftDiags {
			if d.Kind == mirror.DriftSourceNotFound || d.Kind == mirror.DriftTargetNotFound {
				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     svc.Name,
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    d.Message,
					Suggestion: "Ensure the path in @mirror points to an existing Go source file and valid interface",
				})
			}
		}
	}

	return diags
}

// RuleMirrorSignatureDrift checks if @mirror wrapper has drifted from root Go source signatures.
type RuleMirrorSignatureDrift struct{}

func (r *RuleMirrorSignatureDrift) ID() string   { return "E016" }
func (r *RuleMirrorSignatureDrift) Name() string { return "mirror-signature-drift" }
func (r *RuleMirrorSignatureDrift) Description() string {
	return "Enforces synchronization between @mirror wrapper signatures and root Go source"
}
func (r *RuleMirrorSignatureDrift) Category() Category        { return CategoryCorrectness }
func (r *RuleMirrorSignatureDrift) DefaultSeverity() Severity { return SeverityError }
func (r *RuleMirrorSignatureDrift) IsFixable() bool           { return false }

func (r *RuleMirrorSignatureDrift) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil || pass.FilePath == "" {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		if svc.Mirror == nil || svc.Mirror.Source == "" {
			continue
		}

		driftDiags, err := mirror.CheckService(pass.RootDir, pass.FilePath, svc, pass.RootIR.Structs)
		if err != nil {
			continue
		}

		line, col := pass.FindNodePosition(svc.Name, "")
		for _, d := range driftDiags {
			if d.Kind == mirror.DriftParamMismatch || d.Kind == mirror.DriftReturnMismatch ||
				d.Kind == mirror.DriftFieldMismatch || d.Kind == mirror.DriftMethodMissing {
				target := svc.Name
				if d.Method != "" {
					target = fmt.Sprintf("%s.%s", svc.Name, d.Method)
				}

				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     target,
					FilePath:   pass.FilePath,
					Line:       line,
					Column:     col,
					Message:    d.Message,
					Suggestion: "Update wrapper method parameter or DTO field to match the root Go source signature",
				})
			}
		}
	}

	return diags
}
