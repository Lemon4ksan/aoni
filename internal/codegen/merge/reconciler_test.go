// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package merge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/merge"
)

func TestReconciler_AdditiveNewMethod_And_Field(t *testing.T) {
	t.Parallel()

	ours := &ir.RootIR{
		PackageName: "testapi",
		Services: []*ir.ServiceIR{
			{
				Name: "UserAPI",
				Methods: []*ir.MethodIR{
					{
						Name:       "GetUser",
						HTTPMethod: "GET",
						Path:       &ir.PathIR{RawTemplate: "/v1/users/{id}"},
						Params: []*ir.ParamIR{
							{GoName: "id", GoType: ir.GoTypeIR{Name: "string"}},
						},
					},
				},
			},
		},
		Structs: []*ir.StructIR{
			{
				Name: "UserDTO",
				Fields: []*ir.FieldIR{
					{GoName: "ID", WireName: "id", Type: ir.GoTypeIR{Name: "string"}},
				},
			},
		},
	}

	theirs := &ir.RootIR{
		PackageName: "testapi",
		Services: []*ir.ServiceIR{
			{
				Name: "UserAPI",
				Methods: []*ir.MethodIR{
					{
						Name:       "GetUser",
						HTTPMethod: "GET",
						Path:       &ir.PathIR{RawTemplate: "/v1/users/{id}"},
						Params: []*ir.ParamIR{
							{GoName: "id", GoType: ir.GoTypeIR{Name: "string"}},
							{GoName: "expand", GoType: ir.GoTypeIR{Name: "string"}},
						},
					},
					{
						Name:       "ListUsers",
						HTTPMethod: "GET",
						Path:       &ir.PathIR{RawTemplate: "/v1/users"},
					},
				},
			},
		},
		Structs: []*ir.StructIR{
			{
				Name: "UserDTO",
				Fields: []*ir.FieldIR{
					{GoName: "ID", WireName: "id", Type: ir.GoTypeIR{Name: "string"}},
					{GoName: "AvatarBlurhash", WireName: "avatar_blurhash", Type: ir.GoTypeIR{Name: "string"}},
				},
			},
			{
				Name: "CreateUserRequest",
				Fields: []*ir.FieldIR{
					{GoName: "Name", Type: ir.GoTypeIR{Name: "string"}},
				},
			},
		},
	}

	rec := merge.NewReconciler()
	res, err := rec.Reconcile(nil, ours, theirs)
	require.NoError(t, err)

	assert.Equal(t, 0, res.BreakingCount)
	assert.Equal(
		t,
		4,
		res.AdditiveCount,
	) // 1 new method (ListUsers), 1 new param (expand), 1 new field (AvatarBlurhash), 1 new struct (CreateUserRequest)

	assert.Len(t, res.MethodPlans, 2)
	assert.Len(t, res.StructPlans, 2)
}
