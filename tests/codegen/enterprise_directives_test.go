// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/analysis"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/optimizer"
	parserpkg "github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

func TestEnterpriseDirectivesGeneration(t *testing.T) {
	src := `
package exchange

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://api.binance.com/v3"
// @engine fast
type ExchangeAPI interface {
	// 1. Create order with Idempotency Key & HMAC signing
	// @post "order"
	// @idempotent
	// @sign hmac_sha256 key_env="BINANCE_SECRET"
	CreateOrder(ctx context.Context, req *OrderRequest, mods ...aoni.RequestModifier) (*OrderResponse, error)

	// 2. Request Coalescing (Singleflight) on high-throughput ticker endpoint
	// @get "ticker/24hr"
	// @coalesce
	GetTicker(ctx context.Context, symbol string, mods ...aoni.RequestModifier) (*TickerResponse, error)

	// 3. ETag Automaton for 304 conditional request caching
	// @get "exchangeInfo"
	// @etag
	GetExchangeInfo(ctx context.Context, mods ...aoni.RequestModifier) (*ExchangeInfoResponse, error)
}

// @aoni:dto casing=snake_case
type OrderRequest struct {
	Symbol   string
	Side     string
	Quantity float64
	Price    float64
}

// @aoni:dto casing=snake_case
type OrderResponse struct {
	OrderID   uint64
	Status    string
	Executed  float64
}

// @aoni:dto casing=snake_case
type TickerResponse struct {
	Symbol    string
	LastPrice float64
}

// @aoni:dto casing=snake_case
type ExchangeInfoResponse struct {
	Timezone string
}
`

	p := parserpkg.NewParser()
	root, err := p.ParseSource("exchange.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	require.Falsef(t, analysis.HasErrors(diags), "Diagnostics: %v", diags)

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	code, err := emitter.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Verify that generated code parses without syntax errors
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "exchange.gen.go", code, parser.AllErrors)
	require.NoErrorf(t, parseErr, "Generated code syntax error:\n%s", string(code))

	codeStr := string(code)

	// 1. Verify Idempotency modifier injection
	require.Contains(t, codeStr, "allMods = append(allMods, mod.WithIdempotencyKey())")

	// 2. Verify HMAC signing with environment variable key
	require.Contains(t, codeStr, `allMods = append(allMods, mod.WithSignHMAC(os.Getenv("BINANCE_SECRET")))`)

	// 3. Verify Request Coalescing (Singleflight) modifier injection
	require.Contains(t, codeStr, "allMods = append(allMods, mod.WithCoalesce())")

	// 4. Verify ETag 304 Automaton modifier injection
	require.Contains(t, codeStr, "allMods = append(allMods, mod.WithETag())")
}
