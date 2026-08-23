// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package merge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/merge"
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

	res, err := merge.Reconcile(nil, ours, theirs)
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

func TestReconciler_NewService_And_EmptyReconcile(t *testing.T) {
	t.Parallel()

	ours := &ir.RootIR{
		PackageName: "testapi",
		Services: []*ir.ServiceIR{
			{
				Name: "ServiceA",
			},
		},
	}

	theirs := &ir.RootIR{
		PackageName: "testapi",
		Services: []*ir.ServiceIR{
			{
				Name: "ServiceB",
				Methods: []*ir.MethodIR{
					{
						Name:       "DoAction",
						HTTPMethod: "POST",
						Path:       &ir.PathIR{RawTemplate: "/v1/action"},
					},
				},
			},
		},
	}

	res, err := merge.Reconcile(nil, ours, theirs)
	require.NoError(t, err)

	assert.Equal(t, 1, res.AdditiveCount)
	assert.Len(t, res.MethodPlans, 1)
	assert.Equal(t, "ServiceB", res.MethodPlans[0].Service)
}

func TestReconciler_NilIR_Error(t *testing.T) {
	t.Parallel()

	_, err := merge.Reconcile(nil, nil, &ir.RootIR{})
	assert.Error(t, err)

	_, err = merge.Reconcile(nil, &ir.RootIR{}, nil)
	assert.Error(t, err)
}

func TestReconciler_NoDiff_Identical(t *testing.T) {
	t.Parallel()

	root := &ir.RootIR{
		PackageName: "testapi",
		Services: []*ir.ServiceIR{
			{
				Name: "ServiceA",
				Methods: []*ir.MethodIR{
					{
						Name:       "GetData",
						HTTPMethod: "GET",
						Path:       &ir.PathIR{RawTemplate: "/v1/data"},
						Params: []*ir.ParamIR{
							{GoName: "filter", GoType: ir.GoTypeIR{Name: "string"}},
						},
					},
				},
			},
		},
		Structs: []*ir.StructIR{
			{
				Name: "DataDTO",
				Fields: []*ir.FieldIR{
					{GoName: "Value", WireName: "value", Type: ir.GoTypeIR{Name: "int"}},
				},
			},
		},
	}

	res, err := merge.Reconcile(nil, root, root)
	require.NoError(t, err)

	assert.Equal(t, 0, res.AdditiveCount)
	assert.Equal(t, 0, res.BreakingCount)
	assert.Empty(t, res.Deltas)
}
