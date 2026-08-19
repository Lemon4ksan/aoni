// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"os"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

var sensitiveTokens = []string{
	"token",
	"password",
	"secret",
	"api_key",
	"apikey",
	"auth",
	"access_token",
	"refresh_token",
	"private_key",
	"passwd",
}

// RuleSensitiveQueryParam detects sensitive credentials or secrets passed in URL query strings.
type RuleSensitiveQueryParam struct{}

func (r *RuleSensitiveQueryParam) ID() string   { return "S001" }
func (r *RuleSensitiveQueryParam) Name() string { return "sensitive-query-param" }
func (r *RuleSensitiveQueryParam) Description() string {
	return "Detects sensitive credentials or tokens passed in URL query parameters"
}
func (r *RuleSensitiveQueryParam) Category() Category        { return CategorySecurity }
func (r *RuleSensitiveQueryParam) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleSensitiveQueryParam) IsFixable() bool           { return false }

func (r *RuleSensitiveQueryParam) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			for _, p := range m.Params {
				if p.Location != ir.LocQuery {
					continue
				}

				lowerName := strings.ToLower(p.GoName)
				lowerWire := strings.ToLower(p.WireKey)

				isSensitive := false
				matchedToken := ""

				for _, tok := range sensitiveTokens {
					if strings.Contains(lowerName, tok) || strings.Contains(lowerWire, tok) {
						isSensitive = true
						matchedToken = tok

						break
					}
				}

				if isSensitive {
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
							"Sensitive credential '%s' (matched %q) passed in URL query string",
							p.GoName,
							matchedToken,
						),
						Suggestion: "Pass sensitive secrets via Authorization / custom @header or in @form body to prevent access log leakage",
					})
				}
			}
		}
	}

	return diags
}

// RuleHardcodedSigningSecret detects plaintext cryptographic secrets hardcoded in contract directives.
type RuleHardcodedSigningSecret struct{}

func (r *RuleHardcodedSigningSecret) ID() string   { return "S002" }
func (r *RuleHardcodedSigningSecret) Name() string { return "hardcoded-signing-secret" }
func (r *RuleHardcodedSigningSecret) Description() string {
	return "Detects hardcoded cryptographic signing secrets in contract annotations"
}
func (r *RuleHardcodedSigningSecret) Category() Category        { return CategorySecurity }
func (r *RuleHardcodedSigningSecret) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleHardcodedSigningSecret) IsFixable() bool           { return true }

func (r *RuleHardcodedSigningSecret) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			if m.SignHMAC != nil && m.SignHMAC.SecretKey != "" {
				line, col := pass.FindNodePosition(svc.Name, m.Name)
				secretVal := m.SignHMAC.SecretKey
				targetFile := pass.FilePath

				diags = append(diags, Diagnostic{
					RuleID:     r.ID(),
					RuleName:   r.Name(),
					Severity:   r.DefaultSeverity(),
					Category:   r.Category(),
					Target:     fmt.Sprintf("%s.%s", svc.Name, m.Name),
					FilePath:   targetFile,
					Line:       line,
					Column:     col,
					Message:    fmt.Sprintf("Hardcoded signing secret %q found in @sign directive", secretVal),
					Suggestion: "Use `key_env=\"ENV_VAR_NAME\"` to load signing secrets securely from the environment",
					Fix: &Fix{
						Description: fmt.Sprintf("Replace hardcoded secret in %s with key_env", m.Name),
						Apply: func() error {
							content, err := os.ReadFile(targetFile)
							if err != nil {
								return err
							}

							targetPattern := fmt.Sprintf("secret=%q", secretVal)
							replacement := "key_env=\"API_SECRET_KEY\""
							updated := strings.Replace(string(content), targetPattern, replacement, 1)

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

// RuleHeaderCRLFInjectionRisk detects dynamic header variables without URL/header escaping.
type RuleHeaderCRLFInjectionRisk struct{}

func (r *RuleHeaderCRLFInjectionRisk) ID() string   { return "S003" }
func (r *RuleHeaderCRLFInjectionRisk) Name() string { return "header-crlf-injection-risk" }
func (r *RuleHeaderCRLFInjectionRisk) Description() string {
	return "Warns on dynamic header template variables without explicit escaping"
}
func (r *RuleHeaderCRLFInjectionRisk) Category() Category        { return CategorySecurity }
func (r *RuleHeaderCRLFInjectionRisk) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleHeaderCRLFInjectionRisk) IsFixable() bool           { return false }

func (r *RuleHeaderCRLFInjectionRisk) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		for _, m := range svc.Methods {
			for _, h := range m.Headers {
				val := h.StaticValue
				if h.DynamicTemplate != nil {
					val = h.DynamicTemplate.RawTemplate
				}

				if strings.Contains(val, "{") && strings.Contains(val, "}") {
					// Check if any variable has no transformation filter
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

						varContent := val[openIdx+1 : closeIdx]
						if !strings.Contains(varContent, ":") {
							// Check if parameter is a safe numeric/ID type
							var matchedParam *ir.ParamIR
							for _, p := range m.Params {
								if p.GoName == varContent || strings.EqualFold(p.GoName, varContent) ||
									p.WireKey == varContent || strings.EqualFold(p.WireKey, varContent) {
									matchedParam = p
									break
								}
							}

							isSafeType := false
							if matchedParam != nil {
								tName := matchedParam.GoType.Name
								if isNumericType(tName) || strings.HasSuffix(tName, "ID") ||
									strings.HasSuffix(tName, "Code") ||
									tName == "bool" {
									isSafeType = true
								}
							}

							if !isSafeType {
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
										"Dynamic header template variable '{%s}' in header '%s' has no escaping filter",
										varContent,
										h.Key,
									),
									Suggestion: fmt.Sprintf(
										"Use `{%s:path_escape}` or sanitize parameter to prevent CRLF injection",
										varContent,
									),
								})
							}
						}

						start = closeIdx + 1
					}
				}
			}
		}
	}

	return diags
}

// RuleNakedScraperContract detects scraping pipelines on contracts without browser persona or stealth impersonation.
type RuleNakedScraperContract struct{}

func (r *RuleNakedScraperContract) ID() string   { return "S004" }
func (r *RuleNakedScraperContract) Name() string { return "naked-scraper-contract" }
func (r *RuleNakedScraperContract) Description() string {
	return "Warns when HTML scraping extractors are used without browser persona stealth profiles"
}
func (r *RuleNakedScraperContract) Category() Category        { return CategorySecurity }
func (r *RuleNakedScraperContract) DefaultSeverity() Severity { return SeverityWarning }
func (r *RuleNakedScraperContract) IsFixable() bool           { return false }

func (r *RuleNakedScraperContract) Run(pass *Pass) []Diagnostic {
	if pass == nil || pass.RootIR == nil {
		return nil
	}

	var diags []Diagnostic

	for _, svc := range pass.RootIR.Services {
		hasPersona := svc.Persona != "" || svc.TLSSpec != ""

		for _, m := range svc.Methods {
			if m.Extract != nil && !hasPersona {
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
						"Method %s uses scraping @extract without service-level @persona or @tls_spec",
						m.Name,
					),
					Suggestion: "Add `// @persona chrome_133` or `// @tls_spec chrome_auto` to the service interface to evade WAF/bot detection",
				})
			}
		}
	}

	return diags
}

func isNumericType(t string) bool {
	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "byte", "rune":
		return true
	}

	return false
}
