// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

const mistypedDirectivesSource = `
package testapi

import "context"

// @aoni:service
// @base_urrl "https://api.example.com"
type TestMisspelledAPI interface {
	// @unwapr response
	// @posst "items"
	CreateItem(ctx context.Context, name string) (*ItemResponse, error)
}

// @aoni:dtoo casing=snake_case
type ItemResponse struct {
	ID   int64  ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}
`

func TestDiagnostics_FuzzyDirectiveSuggestions(t *testing.T) {
	t.Parallel()

	p := parser.NewParser()
	root, err := p.ParseSource("test.go", []byte(mistypedDirectivesSource))
	require.NoError(t, err)
	require.NotNil(t, root)

	analyzer := analysis.NewAnalyzer()
	diags := analyzer.Analyze(root)

	assert.True(t, analysis.HasErrors(diags))

	diagMsgs := make([]string, 0, len(diags))
	for _, d := range diags {
		diagMsgs = append(diagMsgs, d.Message)
	}

	// Verify fuzzy suggestions for all mistyped directives
	assert.Contains(t, diagMsgs, `unrecognized directive "@base_urrl" (did you mean "@base_url"?)`)
	assert.Contains(t, diagMsgs, `unrecognized directive "@unwapr" (did you mean "@unwrap"?)`)
	assert.Contains(t, diagMsgs, `unrecognized directive "@posst" (did you mean "@post"?)`)
	assert.Contains(t, diagMsgs, `unrecognized directive "@aoni:dtoo" (did you mean "@aoni:dto"?)`)
}
