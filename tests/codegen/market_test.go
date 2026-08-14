// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func TestSteamMarketGeneration(t *testing.T) {
	src := `
package market

import (
	"context"
	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://steamcommunity.com/"
// @engine fast
// @header "X-Requested-With: XMLHttpRequest"
type SteamMarket interface {
	// @post "market/sellitem"
	// @form
	// @header "Referer: https://steamcommunity.com/profiles/{steam_id}/inventory?modal=1&market=1"
	// @check "success == true"
	CreateSellOrder(
		ctx context.Context,
		opts CreateSellOrderOptions,
		steam_id uint64,
		mods ...aoni.RequestModifier,
	) (*CreateSellOrderResponse, error)

	// @post "market/cancelbuyorder"
	// @form
	// @check "success == true"
	CancelBuyOrder(ctx context.Context, buy_orderid uint64, mods ...aoni.RequestModifier) error
}

// @aoni:dto casing=snake_case
type CreateSellOrderOptions struct {
	AppID     uint32
	ContextID int64
	AssetID   uint64
	Amount    int
	Price     int
}

// @aoni:dto casing=snake_case
type CreateSellOrderResponse struct {
	Success bool
	Message string
}

// @aoni:tuple
type GraphPoint struct {
	Price       float64
	Volume      int64
	Description string
}
`

	p := parser.NewParser()
	root, err := p.ParseSource("market.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	require.False(t, analysis.HasErrors(diags))

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	em := emitter.NewEmitter()
	code, err := em.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	codeStr := string(code)
	require.Contains(t, codeStr, "type steamMarketClient struct")
	require.Contains(t, codeStr, "var refBuf [128]byte")
	require.Contains(t, codeStr, "ref = append(ref, \"https://steamcommunity.com/profiles/\"...)")
	require.Contains(t, codeStr, "ref = strconv.AppendUint(ref, uint64(steam_id), 10)")
	require.Contains(t, codeStr, "mod.WithHeader(\"Referer\", bytesconv.B2S(ref))")
	require.Contains(t, codeStr, "opts.AppendFormData(formBuf[:0])")
	require.Contains(t, codeStr, "if !resp.Success")
	require.Contains(t, codeStr, "func (t *GraphPoint) UnmarshalJSON(data []byte) error")

	// Verify Tuple Unmarshaling at runtime
	tupleJSON := `[12.5, 42, "Standard Description"]`
	var pt GraphPoint
	err = json.Unmarshal([]byte(tupleJSON), &pt)
	require.NoError(t, err)
	require.Equal(t, 12.5, pt.Price)
	require.Equal(t, int64(42), pt.Volume)
	require.Equal(t, "Standard Description", pt.Description)
}

type GraphPoint struct {
	Price       float64
	Volume      int64
	Description string
}

func (t *GraphPoint) UnmarshalJSON(data []byte) error {
	var raw [3]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_ = json.Unmarshal(raw[0], &t.Price)
	_ = json.Unmarshal(raw[1], &t.Volume)
	_ = json.Unmarshal(raw[2], &t.Description)
	return nil
}
