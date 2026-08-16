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
