// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package optimizer_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/optimizer"
)

func TestOptimizer_SubRequesterClustering(t *testing.T) {
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
					{
						Name:            "GetBillingInfo",
						TargetRequester: "https://api.pricedb.io/billing/v1",
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
						},
					},
					{
						Name:            "ListDuplicate",
						TargetRequester: "https://sku.pricedb.io/api/",
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
	require.Len(t, svc.SubRequesters, 4)

	// Main default requester
	require.Equal(t, "r", svc.SubRequesters[0].FieldName)
	require.Equal(t, "https://pricedb.io/api/", svc.SubRequesters[0].BaseURL)

	// Subdomain requesters
	require.Equal(t, "sku", svc.SubRequesters[1].FieldName)
	require.Equal(t, "https://sku.pricedb.io/api/", svc.SubRequesters[1].BaseURL)

	require.Equal(t, "spell", svc.SubRequesters[2].FieldName)
	require.Equal(t, "https://spell.pricedb.io/api/", svc.SubRequesters[2].BaseURL)

	// Path-derived requester when host is generic 'api'
	require.Equal(t, "billing", svc.SubRequesters[3].FieldName)
	require.Equal(t, "https://api.pricedb.io/billing/v1", svc.SubRequesters[3].BaseURL)

	// Method targeting
	require.Equal(t, "c.r", svc.Methods[0].TargetRequester)
	require.Equal(t, "c.sku", svc.Methods[1].TargetRequester)
	require.Equal(t, "c.spell", svc.Methods[2].TargetRequester)
	require.Equal(t, "c.billing", svc.Methods[3].TargetRequester)
	require.Equal(t, "c.sku", svc.Methods[4].TargetRequester) // reused existing sub-requester
}

func TestOptimizer_StackAllocationSizing(t *testing.T) {
	opt := optimizer.NewOptimizer()

	root := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name:      "HeavyService",
				BaseURL:   "https://api.example.com/",
				Telemetry: "heavy_telemetry",
				Headers: []ir.HeaderIR{
					{Key: "Content-Type", StaticValue: "application/json"},
				},
				Methods: []*ir.MethodIR{
					{
						Name:        "SimpleGet",
						PayloadKind: ir.PayloadNone,
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{GoName: "id", Location: ir.LocPath, WireKey: "id"},
						},
					},
					{
						Name:        "ComplexForm",
						PayloadKind: ir.PayloadForm,
						Headers: []ir.HeaderIR{
							{Key: "X-Dynamic", DynamicTemplate: &ir.PathIR{RawTemplate: "{token}"}},
							{Key: "X-Static", StaticValue: "static-val"},
						},
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{GoName: "q1", Location: ir.LocQuery, WireKey: "q1"},
							{GoName: "q2", Location: ir.LocQuery, WireKey: "q2"},
							{GoName: "q3", Location: ir.LocQuery, WireKey: "q3"},
							{GoName: "qStruct", Location: ir.LocQueryStruct, WireKey: "filter"},
							{GoName: "field1", Location: ir.LocFormFields, WireKey: "field1"},
							{GoName: "field2", Location: ir.LocFormFields, WireKey: "field2"},
						},
					},
				},
			},
		},
	}

	opt.Optimize(root)

	m0 := root.Services[0].Methods[0]
	require.GreaterOrEqual(t, m0.StackModsSize, optimizer.MinStackModsSize)
	require.Equal(t, optimizer.MinStackBufSize, m0.StackBufSize)

	m1 := root.Services[0].Methods[1]
	require.GreaterOrEqual(t, m1.StackModsSize, 8)
	require.GreaterOrEqual(t, m1.StackBufSize, 128)
}

func TestOptimizer_QueryCanonicalization(t *testing.T) {
	opt := optimizer.NewOptimizer()

	root := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name:    "SearchService",
				BaseURL: "https://api.example.com/",
				Methods: []*ir.MethodIR{
					{
						Name: "Search",
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{GoName: "zebra", Location: ir.LocQuery, WireKey: "zebra"},
							{GoName: "apple", Location: ir.LocQuery, WireKey: "apple"},
							{GoName: "mango", Location: ir.LocQuery, WireKey: "mango"},
						},
					},
				},
			},
		},
	}

	opt.Optimize(root)

	params := root.Services[0].Methods[0].Params
	require.Len(t, params, 4)
	require.Equal(t, "ctx", params[0].GoName)
	require.Equal(t, ir.LocContext, params[0].Location)

	// Preserves declaration order to match Go interface contract
	require.Equal(t, "zebra", params[1].WireKey)
	require.Equal(t, "apple", params[2].WireKey)
	require.Equal(t, "mango", params[3].WireKey)
}

func TestOptimizer_HeaderDeduplication(t *testing.T) {
	opt := optimizer.NewOptimizer()

	root := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name:    "HeaderService",
				BaseURL: "https://api.example.com/",
				Methods: []*ir.MethodIR{
					{
						Name: "FetchWithHeaders",
						Headers: []ir.HeaderIR{
							{Key: "Authorization", StaticValue: "Bearer old"},
							{Key: "X-Trace", StaticValue: "123"},
							{Key: "authorization", StaticValue: "Bearer new"},
						},
					},
				},
			},
		},
	}

	opt.Optimize(root)

	headers := root.Services[0].Methods[0].Headers
	require.Len(t, headers, 2)
	require.Equal(t, "Authorization", headers[0].Key)
	require.Equal(t, "Bearer new", headers[0].StaticValue) // Overridden by latest definition
	require.Equal(t, "X-Trace", headers[1].Key)
}

func TestOptimizer_NilSafety(t *testing.T) {
	opt := optimizer.NewOptimizer()

	// Should not panic on nil root
	require.NotPanics(t, func() {
		opt.Optimize(nil)
	})

	// Should not panic on root with nil services or empty methods
	require.NotPanics(t, func() {
		opt.Optimize(&ir.RootIR{
			Services: []*ir.ServiceIR{
				nil,
				{
					Name:    "EmptyService",
					Methods: []*ir.MethodIR{nil},
				},
			},
		})
	})
}
