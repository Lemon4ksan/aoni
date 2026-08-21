// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"go/ast"
	"net/http"
	"os"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// RuleMissingDTOEncoder checks if structs passed in form/query lack compile-time @aoni:dto encoders.
type RuleMissingDTOEncoder struct{}

func (r *RuleMissingDTOEncoder) ID() string   { return "P001" }
func (r *RuleMissingDTOEncoder) Name() string { return "missing-dto-encoder" }
func (r *RuleMissingDTOEncoder) Description() string {
	return "Suggests @aoni:dto compile-time encoder on structs passed as form or query payloads"
}
func (r *RuleMissingDTOEncoder) Category() Category        { return CategoryPerformance }
func (r *RuleMissingDTOEncoder) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleMissingDTOEncoder) IsFixable() bool           { return true }

func (r *RuleMissingDTOEncoder) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil || pass.ASTFile == nil {
		return nil
	}

	dtoStructNames := make(map[string]bool)
	for _, s := range pass.RootIR.Structs {
		dtoStructNames[s.Name] = true
	}

	usedStructNames := make(map[string]struct{ svc, method string })
	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			for _, p := range m.Params {
				if p.Location == ir.LocQueryStruct || (p.Location == ir.LocBody && m.PayloadKind == ir.PayloadForm) {
					cleanName := strings.TrimPrefix(p.GoType.Name, "*")
					if p.GoType.IsCustomType && cleanName != "" && !strings.Contains(cleanName, ".") {
						usedStructNames[cleanName] = struct{ svc, method string }{svc: svc.Name, method: m.Name}
					}
				}
			}
		}
	}

	var diags []Diagnostic

	for structName, loc := range usedStructNames {
		if !dtoStructNames[structName] {
			line, col := pass.FindNodePosition(structName, "")
			if line == 1 && col == 1 {
				line, col = pass.FindNodePosition(loc.svc, loc.method)
			}

			targetFile := pass.FilePath

			diags = append(diags, Diagnostic{
				RuleID:   r.ID(),
				RuleName: r.Name(),
				Severity: r.DefaultSeverity(),
				Category: r.Category(),
				Target:   fmt.Sprintf("%s (in %s.%s)", structName, loc.svc, loc.method),
				FilePath: targetFile,
				Line:     line,
				Column:   col,
				Message: fmt.Sprintf(
					"Struct %s is passed as payload without compile-time @aoni:dto encoder",
					structName,
				),
				Suggestion: fmt.Sprintf(
					"Add `// @aoni:dto casing=snake_case` above `type %s struct` for 0-allocation serialization",
					structName,
				),
				Fix: &Fix{
					Description: "Add // @aoni:dto to struct " + structName,
					Apply: func() error {
						content, err := os.ReadFile(targetFile)
						if err != nil {
							return err
						}

						lines := strings.Split(string(content), "\n")
						targetPattern := "type " + structName + " struct"

						for i, l := range lines {
							if strings.Contains(l, targetPattern) {
								var indentSb84 strings.Builder

								for _, ch := range l {
									if ch == ' ' || ch == '\t' {
										indentSb84.WriteString(string(ch))
									} else {
										break
									}
								}

								indent := indentSb84.String()

								newLines := make([]string, 0, len(lines)+1)
								newLines = append(newLines, lines[:i]...)
								newLines = append(newLines, indent+"// @aoni:dto casing=snake_case")
								newLines = append(newLines, lines[i:]...)

								// #nosec G703 -- Safe automated rewrite of verified source file
								return os.WriteFile(targetFile, []byte(strings.Join(newLines, "\n")), 0o600)
							}
						}

						return nil
					},
				},
			})
		}
	}

	return diags
}

// RuleAnyParamBoxing checks for parameters typed as any or interface{} that cause interface boxing allocations.
type RuleAnyParamBoxing struct{}

func (r *RuleAnyParamBoxing) ID() string   { return "P002" }
func (r *RuleAnyParamBoxing) Name() string { return "any-param-boxing" }
func (r *RuleAnyParamBoxing) Description() string {
	return "Detects parameters typed as any or interface{} that cause heap allocation boxing"
}
func (r *RuleAnyParamBoxing) Category() Category        { return CategoryPerformance }
func (r *RuleAnyParamBoxing) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleAnyParamBoxing) IsFixable() bool           { return false }

func (r *RuleAnyParamBoxing) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.ASTFile == nil {
		return nil
	}

	var diags []Diagnostic

	ast.Inspect(pass.ASTFile, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		iface, ok := ts.Type.(*ast.InterfaceType)
		if !ok || iface.Methods == nil {
			return true
		}

		for _, m := range iface.Methods.List {
			if len(m.Names) == 0 {
				continue
			}

			methodName := m.Names[0].Name

			funcType, ok := m.Type.(*ast.FuncType)
			if !ok || funcType.Params == nil {
				continue
			}

			for _, pField := range funcType.Params.List {
				isAny := false
				if ident, ok := pField.Type.(*ast.Ident); ok && ident.Name == "any" {
					isAny = true
				} else if ifaceType, ok := pField.Type.(*ast.InterfaceType); ok && len(ifaceType.Methods.List) == 0 {
					isAny = true
				}

				if isAny {
					pos := pass.FileSet.Position(pField.Pos())

					paramNames := ""
					for _, name := range pField.Names {
						if paramNames != "" {
							paramNames += ", "
						}

						paramNames += name.Name
					}

					diags = append(diags, Diagnostic{
						RuleID:   r.ID(),
						RuleName: r.Name(),
						Severity: r.DefaultSeverity(),
						Category: r.Category(),
						Target:   fmt.Sprintf("%s.%s(%s)", ts.Name.Name, methodName, paramNames),
						FilePath: pass.FilePath,
						Line:     pos.Line,
						Column:   pos.Column,
						Message: fmt.Sprintf(
							"Parameter '%s' is typed as 'any' (causes interface boxing allocation)",
							paramNames,
						),
						Suggestion: "Use concrete types or struct DTOs for zero-allocation parameter passing",
					})
				}
			}
		}

		return true
	})

	return diags
}

// RuleUnformattedSliceStrategy checks if query slice parameters lack an explicit serialization strategy.
type RuleUnformattedSliceStrategy struct{}

func (r *RuleUnformattedSliceStrategy) ID() string   { return "P003" }
func (r *RuleUnformattedSliceStrategy) Name() string { return "unformatted-slice-strategy" }
func (r *RuleUnformattedSliceStrategy) Description() string {
	return "Recommends explicit @format strategy on slice query parameters"
}
func (r *RuleUnformattedSliceStrategy) Category() Category        { return CategoryPerformance }
func (r *RuleUnformattedSliceStrategy) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleUnformattedSliceStrategy) IsFixable() bool           { return true }

func (r *RuleUnformattedSliceStrategy) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			for _, p := range m.Params {
				if p.Location == ir.LocQuery && p.GoType.IsSlice && p.Formatter == "" {
					line, col := pass.FindNodePosition(svc.Name, m.Name)
					paramName := p.GoName
					targetFile := pass.FilePath

					diags = append(diags, Diagnostic{
						RuleID:   r.ID(),
						RuleName: r.Name(),
						Severity: r.DefaultSeverity(),
						Category: r.Category(),
						Target:   fmt.Sprintf("%s.%s(%s)", svc.Name, m.Name, paramName),
						FilePath: targetFile,
						Line:     line,
						Column:   col,
						Message: fmt.Sprintf(
							"Slice query parameter '%s' lacks explicit @format strategy",
							paramName,
						),
						Suggestion: "Add `// @format comma` (or pipe, space, bracket) for deterministic slice serialization",
						Fix: &Fix{
							Description: fmt.Sprintf("Add // @format comma for %s in %s", paramName, m.Name),
							Apply: func() error {
								content, err := os.ReadFile(targetFile)
								if err != nil {
									return err
								}

								lines := strings.Split(string(content), "\n")
								for i, l := range lines {
									if strings.Contains(l, m.Name) && strings.Contains(l, paramName) {
										var indentSb242 strings.Builder

										for _, ch := range l {
											if ch == ' ' || ch == '\t' {
												indentSb242.WriteString(string(ch))
											} else {
												break
											}
										}

										indent := indentSb242.String()

										newLines := make([]string, 0, len(lines)+1)
										newLines = append(newLines, lines[:i]...)
										newLines = append(newLines, indent+"// @format comma")
										newLines = append(newLines, lines[i:]...)

										// #nosec G703 -- Safe automated rewrite of verified source file
										return os.WriteFile(targetFile, []byte(strings.Join(newLines, "\n")), 0o600)
									}
								}

								return nil
							},
						},
					})
				}
			}
		}
	}

	return diags
}

// RuleOversizedStackFrame detects when static buffer sizing exceeds 2KB stack threshold.
type RuleOversizedStackFrame struct{}

func (r *RuleOversizedStackFrame) ID() string   { return "P004" }
func (r *RuleOversizedStackFrame) Name() string { return "oversized-stack-frame" }
func (r *RuleOversizedStackFrame) Description() string {
	return "Warns when optimizer calculates static buffer size > 2KB risking goroutine stack growth"
}
func (r *RuleOversizedStackFrame) Category() Category        { return CategoryPerformance }
func (r *RuleOversizedStackFrame) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleOversizedStackFrame) IsFixable() bool           { return false }

func (r *RuleOversizedStackFrame) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			if m.StackBufSize > 2048 || m.StackModsSize > 2048 {
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
						"Method %s requires large static buffer (%d bytes) exceeding 2KB stack threshold",
						m.Name,
						m.StackBufSize,
					),
					Suggestion: "Consider stream-based body uploads or reducing static header template complexity",
				})
			}
		}
	}

	return diags
}

// RuleMissingCoalesceOnHeavyGet suggests @coalesce on heavy GET read models to prevent thundering herd.
type RuleMissingCoalesceOnHeavyGet struct{}

func (r *RuleMissingCoalesceOnHeavyGet) ID() string   { return "P005" }
func (r *RuleMissingCoalesceOnHeavyGet) Name() string { return "missing-coalesce-on-heavy-get" }
func (r *RuleMissingCoalesceOnHeavyGet) Description() string {
	return "Suggests @coalesce request deduplication on read-heavy metadata endpoints"
}
func (r *RuleMissingCoalesceOnHeavyGet) Category() Category        { return CategoryPerformance }
func (r *RuleMissingCoalesceOnHeavyGet) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleMissingCoalesceOnHeavyGet) IsFixable() bool           { return false }

func (r *RuleMissingCoalesceOnHeavyGet) Run(pass *Pass) []Diagnostic {
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
			if !strings.EqualFold(m.HTTPMethod, http.MethodGet) || m.Coalesce {
				continue
			}

			if m.Return != nil && m.Return.SuccessType.Name != "" {
				if s, ok := structMap[m.Return.SuccessType.Name]; ok && len(s.Fields) >= 10 {
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
							"Heavy GET read model %s.%s could benefit from @coalesce deduplication",
							svc.Name,
							m.Name,
						),
						Suggestion: "Add `// @coalesce` to protect against upstream thundering herd effects",
					})
				}
			}
		}
	}

	return diags
}
