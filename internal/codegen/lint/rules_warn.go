// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// RuleParamLifting checks if query/header parameters are duplicated across multiple methods in a service.
type RuleParamLifting struct{}

func (r *RuleParamLifting) ID() string   { return "W001" }
func (r *RuleParamLifting) Name() string { return "param-lifting" }
func (r *RuleParamLifting) Description() string {
	return "Suggests lifting query/header parameters repeated across 4+ methods to service scope"
}
func (r *RuleParamLifting) Category() Category        { return CategoryStyle }
func (r *RuleParamLifting) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleParamLifting) IsFixable() bool           { return false } // Suggestions only

func (r *RuleParamLifting) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		if len(svc.Methods) < 4 {
			continue
		}

		queryCounts := make(map[string]int)
		headerCounts := make(map[string]int)

		for _, m := range svc.Methods {
			for _, p := range m.Params {
				switch p.Location {
				case ir.LocQuery:
					queryCounts[p.WireKey]++
				case ir.LocHeader:
					headerCounts[p.WireKey]++
				}
			}
		}

		for param, count := range queryCounts {
			if count >= 4 && count >= len(svc.Methods)-1 {
				line, col := pass.FindNodePosition(svc.Name, "")

				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   svc.Name,
					FilePath: pass.FilePath,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"Query parameter %q is repeated across %d methods in %s",
						param,
						count,
						svc.Name,
					),
					Suggestion: fmt.Sprintf(
						"Consider lifting to service-level `// @query %q` or configuring in base client modifier",
						param,
					),
				})
			}
		}

		for header, count := range headerCounts {
			if count >= 4 && count >= len(svc.Methods)-1 {
				line, col := pass.FindNodePosition(svc.Name, "")

				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   svc.Name,
					FilePath: pass.FilePath,
					Line:     line,
					Column:   col,
					Message:  fmt.Sprintf("Header %q is repeated across %d methods in %s", header, count, svc.Name),
					Suggestion: fmt.Sprintf(
						"Consider lifting to service-level `// @header %q` or configuring in base client modifier",
						header,
					),
				})
			}
		}
	}

	return diags
}

// RuleDeprecatedAlias checks if deprecated directive aliases are used.
type RuleDeprecatedAlias struct{}

func (r *RuleDeprecatedAlias) ID() string   { return "W002" }
func (r *RuleDeprecatedAlias) Name() string { return "deprecated-alias" }
func (r *RuleDeprecatedAlias) Description() string {
	return "Checks for deprecated directive aliases (e.g., @zstd_decompress -> @zstd)"
}
func (r *RuleDeprecatedAlias) Category() Category        { return CategoryStyle }
func (r *RuleDeprecatedAlias) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleDeprecatedAlias) IsFixable() bool           { return true }

var deprecatedAliases = map[string]string{
	"@zstd_decompress": "@zstd",
}

func (r *RuleDeprecatedAlias) Run(pass *Pass) []Diagnostic {
	if pass == nil || len(pass.SourceBytes) == 0 {
		return nil
	}

	src := string(pass.SourceBytes)
	lines := strings.Split(src, "\n")

	var diags []Diagnostic

	for deprecated, canonical := range deprecatedAliases {
		for i, l := range lines {
			if strings.Contains(l, deprecated) {
				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     pass.FilePath,
					FilePath:   pass.FilePath,
					Line:       i + 1,
					Column:     strings.Index(l, deprecated) + 1,
					Message:    fmt.Sprintf("Deprecated directive alias %q used", deprecated),
					Suggestion: fmt.Sprintf("Use canonical directive %q instead", canonical),
				})
			}
		}
	}

	return diags
}

// RuleHTTPVerbMismatch checks if method names like Get... or Post... conflict with their HTTP verbs.
type RuleHTTPVerbMismatch struct{}

func (r *RuleHTTPVerbMismatch) ID() string   { return "W003" }
func (r *RuleHTTPVerbMismatch) Name() string { return "http-verb-mismatch" }
func (r *RuleHTTPVerbMismatch) Description() string {
	return "Detects method naming conventions conflicting with HTTP verb"
}
func (r *RuleHTTPVerbMismatch) Category() Category        { return CategoryStyle }
func (r *RuleHTTPVerbMismatch) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleHTTPVerbMismatch) IsFixable() bool           { return false } // Suggestions only

func (r *RuleHTTPVerbMismatch) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			if m.Operation != ir.OpHTTP {
				continue
			}

			target := fmt.Sprintf("%s.%s", svc.Name, m.Name)
			name := m.Name
			line, col := pass.FindNodePosition(svc.Name, m.Name)

			// Case 1: Get... or Fetch... annotated with @post without form/multipart/body
			if (strings.HasPrefix(name, "Get") || strings.HasPrefix(name, "Fetch") || strings.HasPrefix(name, "List")) &&
				m.HTTPMethod == "POST" &&
				(m.PayloadKind == ir.PayloadNone || m.PayloadKind == "") {
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
						"Method %q has read-only naming prefix but is annotated with @post without body",
						name,
					),
					Suggestion: "If this is intentional (e.g. Steam WebAPI requirement), suppress with `//vortex:ignore http-verb-mismatch`",
				})
			}
		}
	}

	return diags
}

// RuleRedundantTag checks for redundant @query or @field annotations where default casing infers the exact same wire key.
type RuleRedundantTag struct{}

func (r *RuleRedundantTag) ID() string   { return "W004" }
func (r *RuleRedundantTag) Name() string { return "redundant-tag" }
func (r *RuleRedundantTag) Description() string {
	return "Checks for redundant @query, @field, or @form annotations that match auto-inferred casing"
}
func (r *RuleRedundantTag) Category() Category        { return CategoryStyle }
func (r *RuleRedundantTag) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleRedundantTag) IsFixable() bool           { return true }

func (r *RuleRedundantTag) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil || pass.ASTFile == nil || len(pass.SourceBytes) == 0 {
		return nil
	}

	var diags []Diagnostic

	filePath := filepath.Clean(pass.FilePath)

	type paramKey struct {
		service string
		method  string
		param   string
	}

	paramMap := make(map[paramKey]*ir.ParamIR)
	methodCasingMap := make(map[string]struct{ query, form, svc ir.CasingStrategy })

	for _, svc := range pass.RootIR.Services {
		svcCasing := svc.DefaultCasing
		if svcCasing == "" {
			svcCasing = ir.CasingSnakeCase
		}

		for _, m := range svc.Methods {
			methodCasingMap[svc.Name+"."+m.Name] = struct{ query, form, svc ir.CasingStrategy }{
				query: m.QueryCasing,
				form:  m.FormCasing,
				svc:   svcCasing,
			}

			for _, p := range m.Params {
				paramMap[paramKey{service: svc.Name, method: m.Name, param: p.GoName}] = p
			}
		}
	}

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

			casings := methodCasingMap[ts.Name.Name+"."+methodName]

			for _, pField := range funcType.Params.List {
				for _, pName := range pField.Names {
					pIR := paramMap[paramKey{service: ts.Name.Name, method: methodName, param: pName.Name}]
					if pIR == nil {
						continue
					}

					if pIR.Location != ir.LocQuery && pIR.Location != ir.LocFormFields {
						continue
					}

					effectiveCasing := casings.svc
					if pIR.Location == ir.LocQuery && casings.query != "" {
						effectiveCasing = casings.query
					} else if pIR.Location == ir.LocFormFields && casings.form != "" {
						effectiveCasing = casings.form
					}

					inferred := parser.ToCasing(pIR.GoName, effectiveCasing)
					if inferred == "" || pIR.WireKey != inferred {
						continue
					}

					paramLine := pass.FileSet.Position(pField.Pos()).Line

					var commentsToCheck []string

					for _, cg := range pass.ASTFile.Comments {
						for _, c := range cg.List {
							cLine := pass.FileSet.Position(c.Pos()).Line
							if cLine == paramLine || cLine == paramLine-1 {
								commentsToCheck = append(commentsToCheck, c.Text)
							}
						}
					}

					patterns := []string{
						fmt.Sprintf("// @query %q", pIR.WireKey),
						"// @query " + pIR.WireKey,
						fmt.Sprintf("// @field %q", pIR.WireKey),
						"// @field " + pIR.WireKey,
						fmt.Sprintf("// @form %q", pIR.WireKey),
						"// @form " + pIR.WireKey,
					}

					for _, pat := range patterns {
						matched := false

						for _, commentText := range commentsToCheck {
							if strings.Contains(commentText, pat) {
								matched = true
								break
							}
						}

						if matched {
							target := fmt.Sprintf("%s.%s(%s)", ts.Name.Name, methodName, pIR.GoName)
							tagToRemove := pat
							paramLine := pass.FileSet.Position(pField.Pos()).Line

							diags = append(diags, Diagnostic{
								RuleID:   r.ID(),
								RuleName: r.Name(),
								Severity: r.DefaultSeverity(),
								Category: r.Category(),
								Line:     paramLine,
								Target:   target,
								FilePath: filePath,
								Message: fmt.Sprintf(
									"Redundant directive %q on parameter %q (casing strategy already produces %q)",
									pat,
									pIR.GoName,
									pIR.WireKey,
								),
								Fix: &Fix{
									Description: fmt.Sprintf("Remove redundant %q", pat),
									Apply: func() error {
										latest, err := os.ReadFile(filePath)
										if err != nil {
											return err
										}

										lines := strings.Split(string(latest), "\n")
										if paramLine-1 >= 0 && paramLine-1 < len(lines) {
											l := lines[paramLine-1]
											if strings.Contains(l, tagToRemove) {
												trimmed := strings.TrimRight(
													strings.Replace(l, tagToRemove, "", 1),
													" \t",
												)
												lines[paramLine-1] = trimmed
											}
										}

										// #nosec G703 -- Safe automated rewrite of verified source file
										return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0o600)
									},
								},
							})

							break
						}
					}
				}
			}
		}

		return true
	})

	return diags
}

// RuleCanonicalFormat checks that directives in doc comments and method annotations follow the canonical ordering.
type RuleCanonicalFormat struct{}

func (r *RuleCanonicalFormat) ID() string   { return "W005" }
func (r *RuleCanonicalFormat) Name() string { return "canonical-format" }
func (r *RuleCanonicalFormat) Description() string {
	return "Checks and normalizes directive ordering in doc comments and inline parameter annotations"
}
func (r *RuleCanonicalFormat) Category() Category        { return CategoryStyle }
func (r *RuleCanonicalFormat) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleCanonicalFormat) IsFixable() bool           { return true }

func (r *RuleCanonicalFormat) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.ASTFile == nil || len(pass.SourceBytes) == 0 {
		return nil
	}

	var diags []Diagnostic

	filePath := filepath.Clean(pass.FilePath)

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
			if m.Doc == nil || len(m.Doc.List) <= 1 {
				continue
			}

			startLine := pass.FileSet.Position(m.Doc.Pos()).Line
			endLine := pass.FileSet.Position(m.Doc.End()).Line

			methodName := ""
			if len(m.Names) > 0 {
				methodName = m.Names[0].Name
			}

			var lines []string

			hasDirectives := false

			for _, c := range m.Doc.List {
				text := c.Text
				lines = append(lines, text)

				trimmed := strings.TrimSpace(text)
				if strings.HasPrefix(trimmed, "// @") || strings.HasPrefix(trimmed, "//@") ||
					strings.HasPrefix(trimmed, "//vortex:ignore") {
					hasDirectives = true
				}
			}

			if !hasDirectives {
				continue
			}

			isSorted := sort.SliceIsSorted(lines, func(i, j int) bool {
				return directiveRank(lines[i]) < directiveRank(lines[j])
			})

			if !isSorted {
				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   fmt.Sprintf("%s.%s", ts.Name.Name, methodName),
					FilePath: filePath,
					Line:     startLine,
					Column:   1,
					Message: fmt.Sprintf(
						"Directives in method %s doc comment are not in canonical sequence",
						methodName,
					),
					Fix: &Fix{
						Description: "Reorder directives in " + methodName,
						Apply: func() error {
							latest, err := os.ReadFile(filePath)
							if err != nil {
								return err
							}

							srcLines := strings.Split(string(latest), "\n")
							if startLine-1 < 0 || endLine > len(srcLines) {
								return nil
							}

							var docLines []string
							for i := startLine - 1; i < endLine; i++ {
								docLines = append(docLines, srcLines[i])
							}

							origBlock := strings.Join(docLines, "\n")
							sort.SliceStable(docLines, func(i, j int) bool {
								return directiveRank(docLines[i]) < directiveRank(docLines[j])
							})
							sortedBlock := strings.Join(docLines, "\n")

							updated := strings.Replace(string(latest), origBlock, sortedBlock, 1)
							// #nosec G703 -- Safe automated rewrite of verified source file
							return os.WriteFile(filePath, []byte(updated), 0o600)
						},
					},
				})
			}
		}

		return true
	})

	return diags
}

func directiveRank(line string) int {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "//vortex:ignore") {
		return 100
	}

	if !strings.HasPrefix(line, "// @") && !strings.HasPrefix(line, "//@") {
		return 0
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 50
	}

	dir := strings.TrimPrefix(parts[1], "@")
	dir, _, _ = strings.Cut(dir, ":")

	switch dir {
	case "get",
		"post",
		"put",
		"delete",
		"patch",
		"head",
		"options",
		"rpc",
		"notify",
		"grpc",
		"event",
		"ws_on",
		"ssh_exec",
		"ssh_shell":
		return 10
	case "service",
		"socket",
		"base_url",
		"req",
		"protocol",
		"engine",
		"endpoint",
		"packet",
		"opcode",
		"job_id",
		"heartbeat":
		return 20
	case "form", "multipart", "body", "pipeline", "encoder", "decoder", "codec", "casing", "type_map", "query":
		return 30
	case "preset", "inject", "auth", "referer", "header", "sign_hmac", "cookie", "p0f", "persona", "tls_spec", "ssh":
		return 40
	case "unwrap",
		"expect_status",
		"cache",
		"timeout",
		"retry",
		"circuit",
		"etag",
		"coalesce",
		"idempotent",
		"check",
		"return",
		"status",
		"stream",
		"extract",
		"envelope":
		return 50
	default:
		return 60
	}
}
