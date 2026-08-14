// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
)

func TestOptimizer(t *testing.T) {
	opt := optimizer.NewOptimizer()

	root := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name:    "PriceDB",
				BaseURL: "https://pricedb.io/api/",
				Methods: []*ir.MethodIR{
					{
						Name:            "GetItem",
						TargetRequester: "",
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{GoName: "sku", Location: ir.LocPath},
						},
					},
					{
						Name:            "ListEffects",
						TargetRequester: "https://sku.pricedb.io/api/",
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
						},
					},
					{
						Name:            "GetServiceStats",
						TargetRequester: "https://spell.pricedb.io/api/",
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
						},
					},
				},
			},
		},
	}

	opt.Optimize(root)

	svc := root.Services[0]
	require.Len(t, svc.SubRequesters, 3)

	require.Equal(t, "r", svc.SubRequesters[0].FieldName)
	require.Equal(t, "https://pricedb.io/api/", svc.SubRequesters[0].BaseURL)

	require.Equal(t, "sku", svc.SubRequesters[1].FieldName)
	require.Equal(t, "https://sku.pricedb.io/api/", svc.SubRequesters[1].BaseURL)

	require.Equal(t, "spell", svc.SubRequesters[2].FieldName)
	require.Equal(t, "https://spell.pricedb.io/api/", svc.SubRequesters[2].BaseURL)

	require.Equal(t, "c.r", svc.Methods[0].TargetRequester)
	require.Equal(t, "c.sku", svc.Methods[1].TargetRequester)
	require.Equal(t, "c.spell", svc.Methods[2].TargetRequester)
}
