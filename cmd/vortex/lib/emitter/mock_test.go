// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package emitter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

func TestEmitter_EmitMock(t *testing.T) {
	root := &ir.RootIR{
		PackageName: "user",
		Services: []*ir.ServiceIR{
			{
				Name: "UserAPI",
				Methods: []*ir.MethodIR{
					{
						Name:       "GetUser",
						HTTPMethod: "GET",
						Path:       &ir.PathIR{RawTemplate: "/users/{id}"},
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{GoName: "id", Location: ir.LocPath, GoType: ir.GoTypeIR{Name: "string"}},
						},
						Return: &ir.ReturnIR{
							SuccessType: ir.GoTypeIR{Name: "*UserDTO"},
						},
					},
					{
						Name:       "CreateUser",
						HTTPMethod: "POST",
						Path:       &ir.PathIR{RawTemplate: "/users"},
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
							{
								GoName:   "req",
								Location: ir.LocBody,
								GoType:   ir.GoTypeIR{Name: "CreateUserRequest", IsCustomType: true},
							},
						},
						Return: &ir.ReturnIR{
							SuccessType: ir.GoTypeIR{Name: "*UserDTO"},
						},
					},
				},
			},
		},
	}

	em := &emitter.Emitter{}
	code, err := em.EmitMock(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	src := string(code)
	require.Contains(t, src, "type UserAPIMockServer struct")
	require.Contains(t, src, "NewUserAPIMockServer(t testing.TB)")
	require.Contains(t, src, "OnGetUser")
	require.Contains(t, src, "OnCreateUser")
	require.Contains(t, src, "Client(opts ...aoni.ClientOption)")
	require.Contains(t, src, "Calls(method string)")
}

func TestEmitter_EmitMock_WithFixtures(t *testing.T) {
	root := &ir.RootIR{
		PackageName: "market",
		Services: []*ir.ServiceIR{
			{
				Name: "MarketAPI",
				Methods: []*ir.MethodIR{
					{
						Name:       "GetPrice",
						HTTPMethod: "GET",
						Path:       &ir.PathIR{RawTemplate: "/market/price"},
						Params: []*ir.ParamIR{
							{GoName: "ctx", Location: ir.LocContext},
						},
						Return: &ir.ReturnIR{
							SuccessType: ir.GoTypeIR{Name: "map[string]any"},
						},
						MockFixture: &ir.MockFixtureIR{
							StatusCode:  200,
							ContentType: "application/json",
							Headers: map[string]string{
								"x-custom-source": "vortex-traffic",
							},
							Body: `{"price": 42.50, "currency": "USD"}`,
						},
					},
				},
			},
		},
	}

	em := &emitter.Emitter{}
	code, err := em.EmitMock(root)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	src := string(code)
	require.Contains(t, src, `w.Header().Set("Content-Type", "application/json")`)
	require.Contains(t, src, `w.WriteHeader(200)`)
	require.Contains(t, src, `42.50`)
	require.Contains(t, src, `USD`)
}
