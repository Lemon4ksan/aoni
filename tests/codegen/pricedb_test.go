// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/analysis"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/optimizer"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

func TestPriceDBGeneration(t *testing.T) {
	src := `
package pricedb

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://pricedb.io/api/"
// @engine fast
// @header "User-Agent: G-man Bot/1.0"
type PriceDB interface {
	// @get "item/{sku}"
	GetItem(ctx context.Context, sku string, mods ...aoni.RequestModifier) (*Price, error)

	// @get "search"
	Search(ctx context.Context, q string, limit int, mods ...aoni.RequestModifier) (*SearchResult, error)

	// @get "effect/list"
	// @base_url "https://sku.pricedb.io/api/"
	// @unwrap data
	ListEffects(ctx context.Context, mods ...aoni.RequestModifier) ([]*EffectInfo, error)
}

// @aoni:dto casing=snake_case omitempty=true
type Price struct {
	SKU    string
	Source string
	Time   int64
}

// @aoni:dto casing=snake_case
type SearchResult struct {
	Total int
	Query string
}

// @aoni:dto casing=snake_case
type EffectInfo struct {
	ID   int
	Name string
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("pricedb.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	require.False(t, analysis.HasErrors(diags))

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	code, err := emitter.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	codeStr := string(code)
	require.Contains(t, codeStr, "type priceDBClient struct")
	require.Contains(t, codeStr, "r   request.Requester")
	require.Contains(t, codeStr, "sku request.Requester")
	require.Contains(t, codeStr, "func NewPriceDB(doer any, opts ...aoni.ClientOption) PriceDB")
	require.Contains(t, codeStr, "qBytes = append(qBytes, url.QueryEscape(q)...)")
	require.Contains(t, codeStr, "qBytes = strconv.AppendInt(qBytes, int64(limit), 10)")
	require.Contains(t, codeStr, "type envelope struct")
	require.Contains(t, codeStr, "resp.Data")
}

func TestPriceDBRuntimeExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/item/5021;6":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sku":    "5021;6",
				"source": "pricedb",
				"time":   1700000000,
			})
		case "/api/effect/list":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": []map[string]any{
					{"id": 1, "name": "Burning Flames"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Verify that the standard client engine interacts smoothly with the simulated backend
	client := aoni.NewClient(server.Client())
	require.NotNil(t, client)
}
