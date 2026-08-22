// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/emitter"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/parser"
)

// @aoni:service
type CustomService interface {
	// @get "data/custom"
	// @call customPkg.CustomFetch
	GetCustom(ctx context.Context, id int) (*CustomData, error)

	// @post "data/save"
	// @call customPkg.CustomSave
	SaveCustom(ctx context.Context, req CustomSaveRequest) (*CustomData, error)

	// @get "data/stream"
	// @call customPkg.StreamData
	StreamCustom(ctx context.Context) (io.ReadCloser, error)

	// @get "data/standard"
	GetStandard(ctx context.Context) (*CustomData, error)
}

type CustomData struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CustomSaveRequest struct {
	Name string `json:"name"`
}

func TestCallDirective(t *testing.T) {
	p := parser.NewParser()
	root, err := p.ParseFile("call_test.go")
	require.NoError(t, err)

	code, err := emitter.Emit(root)
	require.NoError(t, err)

	codeStr := string(code)

	// 1. Verify customPkg.CustomFetch dispatch
	require.Contains(t, codeStr, "customPkg.CustomFetch[CustomData](ctx, c.r, \"data/custom\", allMods...)")

	// 2. Verify customPkg.CustomSave dispatch with body
	require.Contains(t, codeStr, "customPkg.CustomSave[CustomData](ctx, c.r, \"data/save\", req, allMods...)")

	// 3. Verify customPkg.StreamData dispatch for io.ReadCloser
	require.Contains(t, codeStr, "customPkg.StreamData(ctx, c.r, \"data/stream\", allMods...)")

	// 4. Verify standard method uses request.GetTo
	require.Contains(t, codeStr, "request.GetTo[CustomData](ctx, c.r, \"data/standard\", allMods...)")
}
