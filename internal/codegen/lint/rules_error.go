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
