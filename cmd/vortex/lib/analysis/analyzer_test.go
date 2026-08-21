// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analysis_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/analysis"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

func TestAnalyzer(t *testing.T) {
	a := analysis.NewAnalyzer()

	t.Run("valid root", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name:    "TestAPI",
					BaseURL: "https://api.test.com",
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
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.False(t, analysis.HasErrors(diags))
	})

	t.Run("missing context parameter", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name: "TestAPI",
					Methods: []*ir.MethodIR{
						{
							Name:       "GetUser",
							HTTPMethod: "GET",
							Params: []*ir.ParamIR{
								{GoName: "id", Location: ir.LocPath},
							},
							Return: &ir.ReturnIR{IsVoid: true},
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.True(t, analysis.HasErrors(diags))
		require.Contains(t, diags[0].Message, "first method parameter must be context.Context")
	})

	t.Run("mismatched path parameter", func(t *testing.T) {
		root := &ir.RootIR{
			Services: []*ir.ServiceIR{
				{
					Name: "TestAPI",
					Methods: []*ir.MethodIR{
						{
							Name:       "GetUser",
							HTTPMethod: "GET",
							Path: &ir.PathIR{
								Segments: []ir.PathSegmentIR{
									{IsVariable: true, VarName: "user_id"},
								},
							},
							Params: []*ir.ParamIR{
								{GoName: "ctx", Location: ir.LocContext},
								{GoName: "wrong_id", Location: ir.LocQuery},
							},
							Return: &ir.ReturnIR{IsVoid: true},
						},
					},
				},
			},
		}

		diags := a.Analyze(root)
		require.True(t, analysis.HasErrors(diags))
		require.Contains(t, diags[0].Message, "path variable {user_id} does not match any method parameter")

		errs := analysis.Errors(diags)
		require.NotEmpty(t, errs)

		warns := analysis.Warnings(diags)
		require.Empty(t, warns)
	})
}
