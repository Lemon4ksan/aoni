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
	return "Checks for deprecated directive aliases (e.g., @zstd_decompress -> @zstd, @aoni:dto -> @dto)"
}
func (r *RuleDeprecatedAlias) Category() Category        { return CategoryStyle }
func (r *RuleDeprecatedAlias) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleDeprecatedAlias) IsFixable() bool           { return true }

var deprecatedAliases = map[string]string{
	"@zstd_decompress":   "@zstd",
	"@brotli_decompress": "@brotli",
	"@gzip_decompress":   "@gzip",
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
				dep := deprecated
				canon := canonical
				targetFile := pass.FilePath

				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     pass.FilePath,
					FilePath:   targetFile,
					Line:       i + 1,
					Column:     strings.Index(l, deprecated) + 1,
					Message:    fmt.Sprintf("Verbose or deprecated directive alias %q used", deprecated),
					Suggestion: fmt.Sprintf("Use canonical directive %q instead", canonical),
					Fix: &Fix{
						Description: fmt.Sprintf("Replace %s with %s", dep, canon),
						Apply: func() error {
							content, err := os.ReadFile(targetFile)
							if err != nil {
								return err
							}

							updated := strings.ReplaceAll(string(content), dep, canon)

							// #nosec G703 -- Safe automated rewrite of verified source file
							return os.WriteFile(targetFile, []byte(updated), 0o600)
						},
					},
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

	checkDocGroup := func(target string, doc *ast.CommentGroup) {
		if doc == nil || len(doc.List) <= 1 {
			return
		}

		startLine := pass.FileSet.Position(doc.Pos()).Line
		endLine := pass.FileSet.Position(doc.End()).Line

		var lines []string

		hasDirectives := false

		for _, c := range doc.List {
			text := c.Text
			lines = append(lines, text)

			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "// @") || strings.HasPrefix(trimmed, "//@") ||
				strings.HasPrefix(trimmed, "//vortex:ignore") {
				hasDirectives = true
			}
		}

		if !hasDirectives {
			return
		}

		isSorted := sort.SliceIsSorted(lines, func(i, j int) bool {
			return directiveRank(lines[i]) < directiveRank(lines[j])
		})

		if !isSorted {
			diags = append(diags, Diagnostic{
				RuleID:     r.ID(),
				RuleName:   r.Name(),
				Severity:   r.DefaultSeverity(),
				Category:   r.Category(),
				Target:     target,
				FilePath:   filePath,
				Line:       startLine,
				Column:     1,
				Message:    fmt.Sprintf("Directives in %s doc comment are not in canonical sequence", target),
				Suggestion: "Reorder directives to follow canonical hierarchy (Scope -> Routing -> Payload -> Headers -> Response -> Resiliency)",
				Fix: &Fix{
					Description: "Reorder directives in " + target,
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

	ast.Inspect(pass.ASTFile, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		// Check type-level doc
		if ts.Doc != nil {
			checkDocGroup(ts.Name.Name, ts.Doc)
		}

		if iface, ok := ts.Type.(*ast.InterfaceType); ok && iface.Methods != nil {
			for _, m := range iface.Methods.List {
				methodName := ""
				if len(m.Names) > 0 {
					methodName = m.Names[0].Name
				}

				checkDocGroup(fmt.Sprintf("%s.%s", ts.Name.Name, methodName), m.Doc)
			}
		}

		return true
	})

	return diags
}

func directiveRank(line string) int {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "//vortex:ignore") {
		return 999
	}

	if !strings.HasPrefix(line, "// @") && !strings.HasPrefix(line, "//@") {
		return 0
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 500
	}

	dir := strings.TrimPrefix(parts[1], "@")
	if idx := strings.Index(dir, ":"); idx != -1 {
		dir = dir[idx+1:]
	}

	switch dir {
	// Service scope
	case "service", "socket":
		return 10
	case "base_url", "endpoint":
		return 20
	case "casing", "type_map":
		return 30
	case "engine", "protocol":
		return 40
	case "persona", "tls_spec", "p0f":
		return 50
	case "auth", "header", "cookie":
		return 60
	case "timeout", "retry", "circuit":
		return 70

	// Method scope
	case "get", "post", "put", "delete", "patch", "head", "options",
		"rpc", "notify", "grpc", "event", "ws_on", "ssh_exec", "ssh_shell":
		return 110
	case "preset", "route_group":
		return 120
	case "form", "multipart", "body", "pipeline", "encoder", "decoder", "codec":
		return 130
	case "query", "referer", "inject", "sign_hmac":
		return 140
	case "unwrap",
		"envelope",
		"expect_status",
		"status_check",
		"check",
		"return",
		"status",
		"stream",
		"extract",
		"error_model":
		return 150
	case "cache", "etag", "coalesce", "idempotent":
		return 160

	// Struct scope
	case "dto", "bitpack", "tuple", "union":
		return 210

	default:
		return 300
	}
}

// RuleDeadDirective detects ineffective, orphaned, or contradictory directives on methods.
type RuleDeadDirective struct{}

func (r *RuleDeadDirective) ID() string   { return "W006" }
func (r *RuleDeadDirective) Name() string { return "dead-directive" }
func (r *RuleDeadDirective) Description() string {
	return "Detects dead or ineffective directives (@unwrap on void return, @cache on mutation methods)"
}
func (r *RuleDeadDirective) Category() Category        { return CategoryStyle }
func (r *RuleDeadDirective) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleDeadDirective) IsFixable() bool           { return true }

func (r *RuleDeadDirective) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	targetFile := pass.FilePath

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			// 1. Dead @unwrap on void return
			if m.UnwrapField != "" && m.Return != nil && m.Return.IsVoid {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				unwrapPattern := "@unwrap"

				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
					FilePath: targetFile,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"Dead directive '@unwrap %s' on method %s returning no payload (void/error only)",
						m.UnwrapField,
						m.Name,
					),
					Suggestion: "Remove @unwrap directive or update return type to struct model",
					Fix: &Fix{
						Description: "Remove dead @unwrap on " + m.Name,
						Apply: func() error {
							return removeLineContaining(targetFile, m.Name, unwrapPattern)
						},
					},
				})
			}

			// 2. Dead @cache on mutation verbs
			isMutation := strings.EqualFold(m.HTTPMethod, "POST") || strings.EqualFold(m.HTTPMethod, "PUT") ||
				strings.EqualFold(m.HTTPMethod, "DELETE") || strings.EqualFold(m.HTTPMethod, "PATCH")

			if isMutation && m.LocalCacheTTL != "" {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				cachePattern := "@cache"

				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   fmt.Sprintf("%s.%s", svc.Name, m.Name),
					FilePath: targetFile,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"Dead directive '@cache' on state-mutating HTTP %s method %s",
						m.HTTPMethod,
						m.Name,
					),
					Suggestion: "Remove @cache directive from mutation methods",
					Fix: &Fix{
						Description: "Remove dead @cache on " + m.Name,
						Apply: func() error {
							return removeLineContaining(targetFile, m.Name, cachePattern)
						},
					},
				})
			}
		}
	}

	return diags
}

// RuleUnusedParam detects method parameters that are never mapped to path, query, header, cookie, or body.
type RuleUnusedParam struct{}

func (r *RuleUnusedParam) ID() string   { return "W007" }
func (r *RuleUnusedParam) Name() string { return "unused-param" }
func (r *RuleUnusedParam) Description() string {
	return "Detects parameters declared in method signatures that are not bound to wire elements"
}
func (r *RuleUnusedParam) Category() Category        { return CategoryStyle }
func (r *RuleUnusedParam) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleUnusedParam) IsFixable() bool           { return false }

func (r *RuleUnusedParam) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			for _, p := range m.Params {
				if p.Location == ir.LocContext || p.Location == ir.LocModifiers || p.Location == ir.LocHandler ||
					p.Location == ir.LocEventHandler {
					continue
				}

				if p.Location == "" {
					line, col := pass.FindNodePosition(svc.Name, m.Name)
					diags = append(diags, Diagnostic{
						RuleID:   r.ID(),
						RuleName: r.Name(),
						Severity: r.DefaultSeverity(),
						Category: r.Category(),
						Target:   fmt.Sprintf("%s.%s(%s)", svc.Name, m.Name, p.GoName),
						FilePath: pass.FilePath,
						Line:     line,
						Column:   col,
						Message: fmt.Sprintf(
							"Parameter '%s' in method %s is not bound to any path, query, header, or body location",
							p.GoName,
							m.Name,
						),
						Suggestion: "Annotate parameter with @query, @header, or remove if unused",
					})
				}
			}
		}
	}

	return diags
}

// RuleInvalidStatusCodeRange validates that status codes in @expect_status and @status are between 100 and 599.
type RuleInvalidStatusCodeRange struct{}

func (r *RuleInvalidStatusCodeRange) ID() string   { return "W008" }
func (r *RuleInvalidStatusCodeRange) Name() string { return "invalid-status-code-range" }
func (r *RuleInvalidStatusCodeRange) Description() string {
	return "Validates that HTTP status codes in contract directives are within standard RFC range 100-599"
}
func (r *RuleInvalidStatusCodeRange) Category() Category        { return CategoryStyle }
func (r *RuleInvalidStatusCodeRange) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleInvalidStatusCodeRange) IsFixable() bool           { return false }

func (r *RuleInvalidStatusCodeRange) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			for _, code := range m.ExpectStatus {
				if code < 100 || code > 599 {
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
							"Status code %d in @expect_status is outside valid RFC range 100-599",
							code,
						),
						Suggestion: "Use a valid HTTP status code (e.g. 200, 201, 204, 404, 500)",
					})
				}
			}

			if m.Return != nil {
				for code := range m.Return.StatusMap {
					if code < 100 || code > 599 {
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
								"Status code %d in @status is outside valid RFC range 100-599",
								code,
							),
							Suggestion: "Use a valid HTTP status code (e.g. 200, 201, 204, 404, 500)",
						})
					}
				}
			}
		}
	}

	for _, u := range pass.RootIR.Unions {
		for code := range u.Variants {
			if code < 100 || code > 599 {
				line, col := pass.FindNodePosition(u.Name, "")
				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   u.Name,
					FilePath: pass.FilePath,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"Status code %d in union %s is outside valid RFC range 100-599",
						code,
						u.Name,
					),
					Suggestion: "Use a valid HTTP status code (e.g. 200, 201, 204, 404, 500)",
				})
			}
		}
	}

	return diags
}

// RuleDuplicateOperationID detects multiple methods bound to the identical operationId or @bind tag.
type RuleDuplicateOperationID struct{}

func (r *RuleDuplicateOperationID) ID() string   { return "W009" }
func (r *RuleDuplicateOperationID) Name() string { return "duplicate-operation-id" }
func (r *RuleDuplicateOperationID) Description() string {
	return "Detects multiple methods bound to identical operationId or @bind tag within the same service"
}
func (r *RuleDuplicateOperationID) Category() Category        { return CategoryStyle }
func (r *RuleDuplicateOperationID) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleDuplicateOperationID) IsFixable() bool           { return false }

func (r *RuleDuplicateOperationID) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		seenOpIDs := make(map[string]string)
		for _, m := range svc.Methods {
			opID := m.OperationID
			if opID == "" {
				continue
			}

			if prevMethod, exists := seenOpIDs[opID]; exists {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   m.Name,
					FilePath: pass.FilePath,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"Method %s has duplicate operation ID %q already bound to method %s",
						m.Name,
						opID,
						prevMethod,
					),
					Suggestion: "Assign a unique `@bind` identifier to method " + m.Name,
				})
			} else {
				seenOpIDs[opID] = m.Name
			}
		}
	}

	return diags
}

// RuleDeprecatedTargetValidation validates that replacement methods specified in @deprecated exist in the service.
type RuleDeprecatedTargetValidation struct{}

func (r *RuleDeprecatedTargetValidation) ID() string   { return "W010" }
func (r *RuleDeprecatedTargetValidation) Name() string { return "deprecated-target-validation" }
func (r *RuleDeprecatedTargetValidation) Description() string {
	return "Validates that replacement method specified in @deprecated directive exists in the contract"
}
func (r *RuleDeprecatedTargetValidation) Category() Category        { return CategoryStyle }
func (r *RuleDeprecatedTargetValidation) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleDeprecatedTargetValidation) IsFixable() bool           { return false }

func (r *RuleDeprecatedTargetValidation) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic
	for _, svc := range pass.RootIR.Services {
		methodNames := make(map[string]bool)
		for _, m := range svc.Methods {
			methodNames[m.Name] = true
			if m.OperationID != "" {
				methodNames[m.OperationID] = true
			}
		}

		for _, m := range svc.Methods {
			if m.Deprecation == nil || m.Deprecation.Replacement == "" {
				continue
			}

			target := m.Deprecation.Replacement
			if !methodNames[target] {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				diags = append(diags, Diagnostic{
					RuleID:   r.ID(),
					RuleName: r.Name(),
					Severity: r.DefaultSeverity(),
					Category: r.Category(),
					Target:   m.Name,
					FilePath: pass.FilePath,
					Line:     line,
					Column:   col,
					Message: fmt.Sprintf(
						"Method %s is deprecated with replacement %q, but %q does not exist in %s",
						m.Name,
						target,
						target,
						svc.Name,
					),
					Suggestion: "Update `replace=\"...\"` in `@deprecated` to refer to an existing method",
				})
			}
		}
	}

	return diags
}

func removeLineContaining(filePath, methodScope, pattern string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	scopeIdx := -1

	for i, l := range lines {
		if strings.Contains(l, methodScope) {
			scopeIdx = i
			break
		}
	}

	if scopeIdx == -1 {
		return nil
	}

	// Look upwards for comment block
	for i := scopeIdx - 1; i >= 0; i-- {
		l := lines[i]
		if !strings.HasPrefix(strings.TrimSpace(l), "//") {
			break
		}

		if strings.Contains(l, pattern) {
			newLines := make([]string, 0, len(lines)-1)
			newLines = append(newLines, lines[:i]...)
			newLines = append(newLines, lines[i+1:]...)

			// #nosec G703 -- Safe automated rewrite of verified source file
			return os.WriteFile(filePath, []byte(strings.Join(newLines, "\n")), 0o600)
		}
	}

	return nil
}
