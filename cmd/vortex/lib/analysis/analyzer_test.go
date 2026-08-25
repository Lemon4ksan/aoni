// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/analysis"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

func TestAnalyzer_DefaultPipeline(t *testing.T) {
	a := analysis.NewAnalyzer()

	t.Run("valid root", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name:    "TestAPI",
					BaseURL: "https://api.test.com",
					Timeout: "10s",
					Methods: []*ir.MethodIR{
						{
							Name:       "GetUser",
							HTTPMethod: "GET",
							Path: &ir.PathIR{
								Segments: []ir.PathSegmentIR{
									{IsVariable: false, Literal: "users/"},
									{IsVariable: true, VarName: "id"},
								},
							},
							Params: []*ir.ParamIR{
								{GoName: "ctx", Location: ir.LocContext},
								{GoName: "id", Location: ir.LocPath},
							},
							Return: &ir.ReturnIR{
								SuccessType: ir.GoTypeIR{Name: "*User"},
							},
							ExpectStatus: []int{200, 204},
						},
					},
				},
			},
			Structs: []*ir.StructIR{
				{
					Name: "User",
					Fields: []*ir.FieldIR{
						{GoName: "ID", WireName: "id"},
						{GoName: "Name", WireName: "name"},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.False(t, analysis.HasErrors(diags))
		assert.Empty(t, analysis.Errors(diags))
	})

	t.Run("duplicate method in service", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name: "UserService",
					Methods: []*ir.MethodIR{
						{
							Name:       "GetUser",
							HTTPMethod: "GET",
							Params:     []*ir.ParamIR{{GoName: "ctx", Location: ir.LocContext}},
							Return:     &ir.ReturnIR{IsVoid: true},
						},
						{
							Name:       "GetUser",
							HTTPMethod: "POST",
							Params:     []*ir.ParamIR{{GoName: "ctx", Location: ir.LocContext}},
							Return:     &ir.ReturnIR{IsVoid: true},
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.True(t, analysis.HasErrors(diags))
		assert.Equal(t, "service/duplicate-method", diags[0].Code)
		assert.Contains(t, diags[0].Message, "duplicate method name \"GetUser\"")
	})

	t.Run("duplicate param in method signature", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name: "OrderService",
					Methods: []*ir.MethodIR{
						{
							Name:       "GetOrder",
							HTTPMethod: "GET",
							Params: []*ir.ParamIR{
								{GoName: "ctx", Location: ir.LocContext},
								{GoName: "id", WireKey: "id", Location: ir.LocQuery},
								{GoName: "id", WireKey: "id2", Location: ir.LocHeader},
							},
							Return: &ir.ReturnIR{IsVoid: true},
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.True(t, analysis.HasErrors(diags))
		assert.Equal(t, "method/duplicate-param", diags[0].Code)
	})

	t.Run("missing wire key for query param", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name: "SearchService",
					Methods: []*ir.MethodIR{
						{
							Name:       "Search",
							HTTPMethod: "GET",
							Params: []*ir.ParamIR{
								{GoName: "ctx", Location: ir.LocContext},
								{GoName: "query", WireKey: "", Location: ir.LocQuery},
							},
							Return: &ir.ReturnIR{IsVoid: true},
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.True(t, analysis.HasErrors(diags))
		assert.Equal(t, "method/empty-wire-key", diags[0].Code)
	})

	t.Run("invalid duration formats", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name:    "InvalidDurationAPI",
					Timeout: "5seconds",
					Circuit: &ir.CircuitBreakerIR{Cooldown: "not_a_duration"},
					Retry:   &ir.RetryIR{Backoff: "bad_backoff", Jitter: "bad_jitter"},
					Methods: []*ir.MethodIR{
						{
							Name:         "Fetch",
							HTTPMethod:   "GET",
							LocalTimeout: "invalid_local_timeout",
							Params:       []*ir.ParamIR{{GoName: "ctx", Location: ir.LocContext}},
							Return:       &ir.ReturnIR{IsVoid: true},
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.True(t, analysis.HasErrors(diags))
		errs := analysis.Errors(diags)
		assert.GreaterOrEqual(t, len(errs), 5)
	})

	t.Run("RFC 9110 GET with body produces warning", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name: "LegacyAPI",
					Methods: []*ir.MethodIR{
						{
							Name:       "GetWithPayload",
							HTTPMethod: "GET",
							Params: []*ir.ParamIR{
								{GoName: "ctx", Location: ir.LocContext},
								{GoName: "body", Location: ir.LocBody},
							},
							Return: &ir.ReturnIR{IsVoid: true},
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.False(t, analysis.HasErrors(diags))
		warns := analysis.Warnings(diags)
		require.Len(t, warns, 1)
		assert.Equal(t, "method/rfc9110-body-discouraged", warns[0].Code)
	})

	t.Run("unrecognized directive with Levenshtein suggestion", func(t *testing.T) {
		root := &ir.RootIR{
			UnrecognizedDirectives: []ir.UnrecognizedDirectiveIR{
				{Target: "TestAPI.GetUser", Name: "gtt"},
			},
		}

		diags := a.Analyze(root)
		require.True(t, analysis.HasErrors(diags))
		assert.Equal(t, "directive/unrecognized", diags[0].Code)
		assert.Equal(t, "@get", diags[0].Suggestion)
	})
}

func TestEngine_CustomPipeline(t *testing.T) {
	// Build an isolated, custom engine for e.g. Python / TypeScript SDK generation target
	engine := analysis.NewEngine()

	customRuleCalled := false
	engine.AddMethodRules(func(ctx *analysis.Context, svc *ir.ServiceIR, m *ir.MethodIR) {
		customRuleCalled = true

		if m.Name == "ForbiddenName" {
			ctx.Error("custom/forbidden", ctx.Target(svc.Name, m.Name), "method name is forbidden in target SDK")
		}
	})

	root := &ir.RootIR{
		Services: []*ir.ServiceIR{
			{
				Name: "CustomService",
				Methods: []*ir.MethodIR{
					{Name: "ForbiddenName"},
				},
			},
		},
	}

	analyzer := analysis.NewCustomAnalyzer(engine)
	diags := analyzer.Analyze(root)

	assert.True(t, customRuleCalled)
	require.True(t, analysis.HasErrors(diags))
	assert.Equal(t, "custom/forbidden", diags[0].Code)
}
