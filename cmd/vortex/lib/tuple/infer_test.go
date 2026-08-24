// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tuple_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/tuple"
)

func TestInferTupleFromJSON_FlatArray(t *testing.T) {
	payload := []byte(`[[["item-123", "Gemini 1.5 Pro", "Foundation Model"]]]`)

	inf, err := tuple.InferTupleFromJSON("ModelInfo", payload)
	require.NoError(t, err)
	require.NotNil(t, inf)
	require.Equal(t, "ModelInfo", inf.StructName)
	require.Len(t, inf.Fields, 3)

	require.Equal(t, "ID", inf.Fields[0].Name)
	require.Equal(t, "string", inf.Fields[0].GoType)
	require.Equal(t, "0", inf.Fields[0].TagPath)

	require.Equal(t, "Name", inf.Fields[1].Name)
	require.Equal(t, "string", inf.Fields[1].GoType)
	require.Equal(t, "1", inf.Fields[1].TagPath)

	require.Equal(t, "Description", inf.Fields[2].Name)
	require.Equal(t, "string", inf.Fields[2].GoType)
	require.Equal(t, "2", inf.Fields[2].TagPath)
}

func TestInferTupleFromJSON_Heuristics(t *testing.T) {
	payload := []byte(`[
		"https://example.com/avatar.png",
		"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
		true,
		1718000000000,
		"2026-08-17T12:00:00Z"
	]`)

	inf, err := tuple.InferTupleFromJSON("UserProfileTuple", payload)
	require.NoError(t, err)
	require.NotNil(t, inf)
	require.Len(t, inf.Fields, 5)

	require.Equal(t, "ImageURL", inf.Fields[0].Name)
	require.Equal(t, "string", inf.Fields[0].GoType)

	require.Equal(t, "Token", inf.Fields[1].Name)
	require.Equal(t, "string", inf.Fields[1].GoType)

	require.Equal(t, "IsEnabled", inf.Fields[2].Name)
	require.Equal(t, "bool", inf.Fields[2].GoType)

	require.Equal(t, "TimestampMs", inf.Fields[3].Name)
	require.Equal(t, "int64", inf.Fields[3].GoType)

	require.Equal(t, "CreatedAt", inf.Fields[4].Name)
	require.Equal(t, "string", inf.Fields[4].GoType)
}

func TestInferTupleFromJSON_JSPBArray(t *testing.T) {
	payload := []byte(`[5, [], [[[4], 1]], null, null, [[1, 5]]]`)

	inf, err := tuple.InferTupleFromJSON("CounterResponse", payload)
	require.NoError(t, err)
	require.NotNil(t, inf)
	require.Equal(t, "CounterResponse", inf.StructName)
	require.NotEmpty(t, inf.Fields)

	// Verify root value field at index 0
	require.Equal(t, "Value", inf.Fields[0].Name)
	require.Equal(t, "0", inf.Fields[0].TagPath)
	require.Equal(t, "int64", inf.Fields[0].GoType)
}
