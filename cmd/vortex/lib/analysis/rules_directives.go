// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis

import (
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/spec"
)

// AllKnownDirectives holds a flattened list of all registered DSL directive names and aliases.
var AllKnownDirectives = func() []string {
	var list []string
	for _, d := range spec.Registry {
		list = append(list, d.Name)
		list = append(list, d.Aliases...)
	}

	return list
}()

// RuleUnrecognizedDirectives reports unknown DSL directives with closest typo suggestions.
func RuleUnrecognizedDirectives(ctx *Context, root *ir.RootIR) {
	for _, ud := range root.UnrecognizedDirectives {
		msg := fmt.Sprintf("unrecognized directive \"@%s\"", ud.Name)

		suggestion := FindClosestDirective(ud.Name)
		if suggestion != "" {
			msg = fmt.Sprintf("unrecognized directive \"@%s\" (did you mean \"@%s\"?)", ud.Name, suggestion)
			suggestion = "@" + suggestion
		}

		ctx.ReportWithSuggestion(
			"directive/unrecognized",
			SeverityError,
			ud.Target,
			msg,
			suggestion,
		)
	}
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

			d[i][j] = min(sub, min(ins, del))
		}
	}

	return d[n][m]
}
