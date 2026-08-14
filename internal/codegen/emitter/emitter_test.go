// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/ir"
)

func TestEmitter(t *testing.T) {
	root := &ir.RootIR{
		PackageName: "githubapi",
		Services: []*ir.ServiceIR{
			{
				Name:    "GitHubAPI",
				BaseURL: "https://api.github.com",
				Engine:  ir.EngineFast,
				SubRequesters: []ir.SubRequesterIR{
					{FieldName: "r", BaseURL: "https://api.github.com"},
				},
				Headers: []ir.HeaderIR{
					{Key: "User-Agent", StaticValue: "Aoni-Bot"},
				},
				Methods: []*ir.MethodIR{
					{
						Name:            "GetUser",
						HTTPMethod:      "GET",
						TargetRequester: "c.r",
						Path: &ir.PathIR{
							RawTemplate: "users/{username}",
							Segments: []ir.PathSegmentIR{
								{IsVariable: false, Literal: "users/"},
								{IsVariable: true, VarName: "username"},
							},
						},
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext, GoType: ir.GoTypeIR{Name: "context.Context"}},
							{
								GoName:   "username",
								Location: ir.LocPath,
								WireKey:  "username",
								GoType:   ir.GoTypeIR{Name: "string"},
							},
							{
								GoName:   "mods",
								Location: ir.LocModifiers,
								GoType:   ir.GoTypeIR{Name: "...aoni.RequestModifier"},
							},
						},
						Return: &ir.ReturnIR{
							SuccessType: ir.GoTypeIR{Name: "*User", IsPointer: true},
						},
						StackModsSize: 4,
					},
					{
						Name:            "SearchUsers",
						HTTPMethod:      "GET",
						TargetRequester: "c.r",
						Path: &ir.PathIR{
							RawTemplate: "search/users",
							Segments: []ir.PathSegmentIR{
								{IsVariable: false, Literal: "search/users"},
							},
						},
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext, GoType: ir.GoTypeIR{Name: "context.Context"}},
							{
								GoName:    "q",
								Location:  ir.LocQuery,
								WireKey:   "q",
								GoType:    ir.GoTypeIR{Name: "string"},
								Formatter: ir.FormatQueryEscaped,
							},
							{
								GoName:    "limit",
								Location:  ir.LocQuery,
								WireKey:   "limit",
								GoType:    ir.GoTypeIR{Name: "int"},
								Formatter: ir.FormatIntAppend,
							},
						},
						Return: &ir.ReturnIR{
							SuccessType: ir.GoTypeIR{Name: "*SearchResult", IsPointer: true},
						},
						StackModsSize: 4,
						StackBufSize:  64,
					},
				},
			},
		},
		Structs: []*ir.StructIR{
			{
				Name:            "User",
				GenValueEncoder: true,
				Fields: []*ir.FieldIR{
					{GoName: "ID", WireName: "id", Type: ir.GoTypeIR{Name: "int64"}},
					{GoName: "Login", WireName: "login", Type: ir.GoTypeIR{Name: "string"}},
				},
			},
		},
		Tuples: []*ir.TupleIR{
			{
				Name: "Point",
				Fields: []ir.TupleFieldIR{
					{Index: 0, GoName: "X", Type: ir.GoTypeIR{Name: "float64"}},
					{Index: 1, GoName: "Y", Type: ir.GoTypeIR{Name: "float64"}},
				},
			},
		},
	}

	em := emitter.NewEmitter()
	code, err := em.Emit(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	codeStr := string(code)
	require.Contains(t, codeStr, "package githubapi")
	require.Contains(t, codeStr, "type gitHubAPIClient struct")
	require.Contains(t, codeStr, "func NewGitHubAPI(doer any, opts ...aoni.ClientOption) GitHubAPI")
	require.Contains(
		t,
		codeStr,
		"func (c *gitHubAPIClient) GetUser(ctx context.Context, username string, mods ...aoni.RequestModifier) (*User, error)",
	)
	require.Contains(
		t,
		codeStr,
		"func (c *gitHubAPIClient) SearchUsers(ctx context.Context, q string, limit int) (*SearchResult, error)",
	)
	require.Contains(t, codeStr, "func (r *User) EncodeValues(vals url.Values)")
	require.Contains(t, codeStr, "func (t *Point) UnmarshalJSON(data []byte) error")
}
