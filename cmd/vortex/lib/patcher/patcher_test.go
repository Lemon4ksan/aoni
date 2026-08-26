// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package patcher_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/merge"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/patcher"
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

	patched, err := patcher.PatchBytes([]byte(originalSource), plan)
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

func TestPatcher_PatchExistingStruct_AddField(t *testing.T) {
	t.Parallel()

	originalSource := `package api

type UserDTO struct {
	ID string ` + "`" + `query:"id"` + "`" + `
}
`

	plan := &merge.ReconcileResult{
		StructPlans: []merge.StructMergePlan{
			{
				StructName: "UserDTO",
				IsNew:      false,
				NewFields: []*ir.FieldIR{
					{GoName: "Avatar", WireName: "avatar_url", Type: ir.GoTypeIR{Name: "string"}},
				},
			},
		},
	}

	patched, err := patcher.PatchBytes([]byte(originalSource), plan)
	require.NoError(t, err)

	patchedStr := string(patched)
	assert.Contains(t, patchedStr, "ID")
	assert.Contains(t, patchedStr, "`query:\"id\"`")
	assert.Contains(t, patchedStr, "Avatar")
	assert.Contains(t, patchedStr, "`query:\"avatar_url,omitempty\"`")
}

func TestPatcher_PatchFile_OnDisk(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "api.go")

	originalSource := `package testapi

type Service interface {
	Do() error
}
`
	require.NoError(t, os.WriteFile(filePath, []byte(originalSource), 0o600))

	plan := &merge.ReconcileResult{
		MethodPlans: []merge.MethodMergePlan{
			{
				Service: "Service",
				IsNew:   true,
				TargetMethod: &ir.MethodIR{
					Name:       "Action",
					HTTPMethod: "POST",
					Path:       &ir.PathIR{RawTemplate: "/action"},
				},
			},
		},
	}

	err := patcher.PatchFile(filePath, plan)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(
		t,
		string(data),
		"Action(ctx context.Context, mods ...aoni.RequestModifier) (*json.RawMessage, error)",
	)
}

func TestPatcher_InvalidGoSyntax_Error(t *testing.T) {
	t.Parallel()

	invalidSource := `package api ??? invalid go syntax`

	_, err := patcher.PatchBytes([]byte(invalidSource), &merge.ReconcileResult{})
	assert.Error(t, err)
}
