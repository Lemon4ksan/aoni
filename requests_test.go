// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetJSON(t *testing.T) {
	t.Parallel()
	expected := testPayload{Message: "hello", Status: http.StatusOK}

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	})

	result, err := GetJSON[testPayload](t.Context(), client, "/json")
	require.NoError(t, err)

	assert.Equal(t, expected.Message, result.Message)
	assert.Equal(t, expected.Status, result.Status)
}

func TestClient_PostJSON(t *testing.T) {
	t.Parallel()
	input := testPayload{Message: "sending", Status: 1}
	response := testPayload{Message: "received", Status: 2}

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body testPayload
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := PostJSON[testPayload, testPayload](t.Context(), client, "/post", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_PutJSON(t *testing.T) {
	t.Parallel()
	input := testPayload{Message: "sending-put", Status: 1}
	response := testPayload{Message: "received-put", Status: 2}

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body testPayload
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := PutJSON[testPayload, testPayload](t.Context(), client, "/put", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_PatchJSON(t *testing.T) {
	t.Parallel()
	input := testPayload{Message: "sending-patch", Status: 1}
	response := testPayload{Message: "received-patch", Status: 2}

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body testPayload
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := PatchJSON[testPayload, testPayload](t.Context(), client, "/patch", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_DeleteJSON(t *testing.T) {
	t.Parallel()
	input := testPayload{Message: "deleting", Status: 1}
	response := testPayload{Message: "deleted", Status: 2}

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body testPayload
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := DeleteJSON[testPayload, testPayload](t.Context(), client, "/delete", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_DeleteJSON_NilPayload(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)

		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		assert.ErrorIs(t, err, io.EOF)

		w.WriteHeader(http.StatusNoContent)
	})

	_, err := DeleteJSON[*testPayload, any](t.Context(), client, "/delete-nil", nil)
	require.NoError(t, err)
}
