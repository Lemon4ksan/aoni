// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"fmt"
	"os"
	"strings"

	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
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

			paramNames := make(map[string]bool, len(m.Params))
			for _, p := range m.Params {
				paramNames[p.GoName] = true
				paramNames[strings.ToLower(p.GoName)] = true
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

// RuleInvalidBitpack checks that @aoni:bitpack struct fields have valid bit widths and don't overflow their underlying types.
type RuleInvalidBitpack struct{}

func (r *RuleInvalidBitpack) ID() string   { return "E006" }
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
