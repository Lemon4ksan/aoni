// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lint

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

var (
	// Matches //vortex:ignore <rules> [-- reason]
	rxVortexIgnore = regexp.MustCompile(`(?i)//\s*vortex:ignore\s+(.*)`)

	// Matches //vortex:ignore-service <rules> [-- reason]
	rxVortexIgnoreService = regexp.MustCompile(`(?i)//\s*vortex:ignore-service\s+(.*)`)

	// Matches //nolint[:rules] [-- reason]
	rxNoLint = regexp.MustCompile(`(?i)//\s*nolint(?::([^\s]+))?`)
)

// IgnoreMap stores rule suppressions mapped by target scope or line number.
type IgnoreMap struct {
	// ServiceIgnores maps serviceName -> set of ignored rule IDs/names
	ServiceIgnores map[string]map[string]bool

	// TargetIgnores maps "service.Method" -> set of ignored rule IDs/names
	TargetIgnores map[string]map[string]bool

	// LineIgnores maps line number -> set of ignored rule IDs/names
	LineIgnores map[int]map[string]bool

	// GlobalIgnores contains rules suppressed for the entire file
	GlobalIgnores map[string]bool
}

// NewIgnoreMap constructs an empty IgnoreMap.
func NewIgnoreMap() *IgnoreMap {
	return &IgnoreMap{
		ServiceIgnores: make(map[string]map[string]bool),
		TargetIgnores:  make(map[string]map[string]bool),
		LineIgnores:    make(map[int]map[string]bool),
		GlobalIgnores:  make(map[string]bool),
	}
}

// ParseIgnores scans AST comments and builds an [IgnoreMap].
func ParseIgnores(fset *token.FileSet, file *ast.File) *IgnoreMap {
	ignores := NewIgnoreMap()
	if file == nil || fset == nil {
		return ignores
	}

	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := comment.Text
			pos := fset.Position(comment.Pos())

			// 1. Check //vortex:ignore-service
			if m := rxVortexIgnoreService.FindStringSubmatch(text); len(m) > 1 {
				ruleStr, _, _ := strings.Cut(m[1], "--")

				rules := parseRuleList(ruleStr)
				for _, r := range rules {
					ignores.GlobalIgnores[r] = true
				}
			}

			// 2. Check //vortex:ignore
			if m := rxVortexIgnore.FindStringSubmatch(text); len(m) > 1 {
				ruleStr, _, _ := strings.Cut(m[1], "--")
				rules := parseRuleList(ruleStr)

				if ignores.LineIgnores[pos.Line] == nil {
					ignores.LineIgnores[pos.Line] = make(map[string]bool)
				}

				// Also suppress next line (for doc comment immediately preceding declaration)
				if ignores.LineIgnores[pos.Line+1] == nil {
					ignores.LineIgnores[pos.Line+1] = make(map[string]bool)
				}

				if ignores.LineIgnores[pos.Line+2] == nil {
					ignores.LineIgnores[pos.Line+2] = make(map[string]bool)
				}

				for _, r := range rules {
					ignores.LineIgnores[pos.Line][r] = true
					ignores.LineIgnores[pos.Line+1][r] = true
					ignores.LineIgnores[pos.Line+2][r] = true
				}
			}

			// 3. Check standard Go //nolint or //nolint:rule1,rule2
			if m := rxNoLint.FindStringSubmatch(text); len(m) > 0 {
				var rules []string
				if len(m) > 1 && m[1] != "" {
					rules = parseRuleList(m[1])
				} else {
					rules = []string{"all"}
				}

				if ignores.LineIgnores[pos.Line] == nil {
					ignores.LineIgnores[pos.Line] = make(map[string]bool)
				}

				if ignores.LineIgnores[pos.Line+1] == nil {
					ignores.LineIgnores[pos.Line+1] = make(map[string]bool)
				}

				if ignores.LineIgnores[pos.Line+2] == nil {
					ignores.LineIgnores[pos.Line+2] = make(map[string]bool)
				}

				for _, r := range rules {
					ignores.LineIgnores[pos.Line][r] = true
					ignores.LineIgnores[pos.Line+1][r] = true
					ignores.LineIgnores[pos.Line+2][r] = true
				}
			}
		}
	}

	return ignores
}

// IsIgnored checks if a diagnostic matching ruleID / ruleName is suppressed for the given target and line.
func (m *IgnoreMap) IsIgnored(ruleID, ruleName, target string, line int) bool {
	if m == nil {
		return false
	}

	id := strings.ToLower(ruleID)
	name := strings.ToLower(ruleName)

	// 1. File / global level
	if m.GlobalIgnores["all"] || m.GlobalIgnores[id] || m.GlobalIgnores[name] {
		return true
	}

	// 2. Target level
	if target != "" {
		if rules, ok := m.TargetIgnores[target]; ok {
			if rules["all"] || rules[id] || rules[name] {
				return true
			}
		}

		// Check service prefix if target is Service.Method
		if parts := strings.Split(target, "."); len(parts) > 1 {
			svcName := parts[0]
			if rules, ok := m.ServiceIgnores[svcName]; ok {
				if rules["all"] || rules[id] || rules[name] {
					return true
				}
			}
		}
	}

	// 3. Line level
	if line > 0 {
		if rules, ok := m.LineIgnores[line]; ok {
			if rules["all"] || rules[id] || rules[name] {
				return true
			}
		}
	}

	return false
}

func parseRuleList(s string) []string {
	parts := strings.Split(s, ",")

	var rules []string
	for _, p := range parts {
		clean := strings.ToLower(strings.TrimSpace(p))
		if clean != "" {
			rules = append(rules, clean)
		}
	}

	return rules
}
