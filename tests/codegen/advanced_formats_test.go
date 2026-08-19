// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
	parserpkg "github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func TestAdvancedFormatsAndTypeMap(t *testing.T) {
	src := `
package fintech

import (
	"context"
	"time"

	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://api.fintech.com/v1"
// @type_map time.Time -> unix_s
// @type_map []string -> comma
type BillingAPI interface {
	// @get "transactions"
	GetTransactions(
		ctx context.Context,
		from time.Time, // @format unix_ms
		to time.Time, // @format layout="2006-01-02"
		statuses []string, // @format comma
		categories []string, // @format pipe
		accountIDs []int64, // @format comma
		includePending bool, // @format bool_int
		compact bool, // @format flag
		mods ...aoni.RequestModifier,
	) (*TransactionList, error)

	// @post "config/import"
	// @return body | custom=yamlDecoder
	ImportConfig(ctx context.Context, req *ImportRequest, mods ...aoni.RequestModifier) (*ImportResponse, error)
}

// @aoni:dto casing=snake_case
type TransactionList struct {
	Total int
}

// @aoni:dto casing=snake_case
type ImportRequest struct {
	RawConfig string
}

// @aoni:dto casing=snake_case
type ImportResponse struct {
	Success bool
}
`

	p := parserpkg.NewParser()
	root, err := p.ParseSource("fintech.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	require.False(t, analysis.HasErrors(diags), "Diagnostics: %v", diags)

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	em := emitter.NewEmitter()
	code, err := em.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Verify that generated code parses without syntax errors
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "fintech.gen.go", code, parser.AllErrors)
	require.NoError(t, parseErr, "Generated code syntax error:\n%s", string(code))

	codeStr := string(code)

	// 1. Verify unix_ms formatting
	require.Contains(t, codeStr, "qBytes = strconv.AppendInt(qBytes, from.UnixMilli(), 10)")

	// 2. Verify custom date layout
	require.Contains(t, codeStr, `qBytes = append(qBytes, url.QueryEscape(to.Format("2006-01-02"))...)`)

	// 3. Verify slice formatting with comma delimiter for strings (url.QueryEscape)
	require.Contains(t, codeStr, "qBytes = append(qBytes, url.QueryEscape(v)...)")

	// 4. Verify slice formatting with comma delimiter for ints (0 alloc strconv.AppendInt)
	require.Contains(t, codeStr, "qBytes = strconv.AppendInt(qBytes, int64(v), 10)")

	// 5. Verify slice formatting with pipe delimiter
	require.Contains(t, codeStr, "qBytes = append(qBytes, '|')")

	// 6. Verify bool_int formatting
	require.Contains(t, codeStr, "qBytes = append(qBytes, '1')")

	// 7. Verify custom pipeline stage
	require.Contains(t, codeStr, "return yamlDecoder(stageIn)")
}
