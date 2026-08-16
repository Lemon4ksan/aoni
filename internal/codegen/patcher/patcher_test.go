// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package patcher_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/ir"
	"github.com/lemon4ksan/aoni/internal/codegen/merge"
	"github.com/lemon4ksan/aoni/internal/codegen/patcher"
)

func TestPatcher_NonDestructive_AST_Patch(t *testing.T) {
	t.Parallel()

	originalSource := `// Package api defines the test API.
package api

import (
	"context"
	"encoding/json"

	"github.com/lemon4ksan/aoni"
)

// UserAPI provides methods for the user service.
//
// @aoni:service casing=snake_case engine=fast
// @base_url "https://api.example.com"
type UserAPI interface {
	// GetUser retrieves a user by ID.
	//
	// @get "/v1/users/{id}"
	GetUser(ctx context.Context, id string, mods ...aoni.RequestModifier) (*json.RawMessage, error)
}
`

	plan := &merge.ReconcileResult{
		MethodPlans: []merge.MethodMergePlan{
			{
				Service: "UserAPI",
				IsNew:   true,
				TargetMethod: &ir.MethodIR{
					Name:       "ListUsers",
					HTTPMethod: "GET",
					Path:       &ir.PathIR{RawTemplate: "/v1/users"},
					Params: []*ir.ParamIR{
						{GoName: "limit", GoType: ir.GoTypeIR{Name: "int"}},
					},
				},
			},
		},
		StructPlans: []merge.StructMergePlan{
			{
				StructName: "ListUsersRequest",
				IsNew:      true,
				Target: &ir.StructIR{
					Name: "ListUsersRequest",
					Fields: []*ir.FieldIR{
						{GoName: "Limit", WireName: "limit", Type: ir.GoTypeIR{Name: "int"}},
					},
				},
			},
		},
	}

	p := patcher.NewPatcher()
	patched, err := p.PatchBytes([]byte(originalSource), plan)
	require.NoError(t, err)

	patchedStr := string(patched)

	// Verify original content preserved
	assert.Contains(t, patchedStr, "// UserAPI provides methods for the user service.")
	assert.Contains(t, patchedStr, "// @get \"/v1/users/{id}\"")
	assert.Contains(t, patchedStr, "GetUser(ctx context.Context, id string, mods ...aoni.RequestModifier)")

	// Verify new method inserted
	assert.Contains(t, patchedStr, "ListUsers(ctx context.Context, limit int, mods ...aoni.RequestModifier)")

	// Verify new struct inserted
	assert.Contains(t, patchedStr, "type ListUsersRequest struct")
	assert.Contains(t, patchedStr, "Limit int")
}
