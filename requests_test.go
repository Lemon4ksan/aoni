// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetJSON(t *testing.T) {
	expected := testPayload{Message: "hello", Status: http.StatusOK}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := GetJSON[testPayload](context.Background(), client, "/json")
	if err != nil {
		t.Fatalf("GetJSON failed: %v", err)
	}

	if result.Message != expected.Message || result.Status != expected.Status {
		t.Errorf("decoded struct mismatch. got %+v, want %+v", result, expected)
	}
}

func TestClient_PostJSON(t *testing.T) {
	input := testPayload{Message: "sending", Status: 1}
	response := testPayload{Message: "received", Status: 2}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected application/json content type")
		}

		var body testPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if body.Message != input.Message {
			t.Errorf("request body mismatch: got %s, want %s", body.Message, input.Message)
		}

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := PostJSON[testPayload, testPayload](context.Background(), client, "/post", input)
	if err != nil {
		t.Fatalf("PostJSON failed: %v", err)
	}

	if result.Message != response.Message {
		t.Errorf("response mismatch: got %s, want %s", result.Message, response.Message)
	}
}

func TestClient_PutJSON(t *testing.T) {
	input := testPayload{Message: "sending-put", Status: 1}
	response := testPayload{Message: "received-put", Status: 2}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected application/json content type")
		}

		var body testPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if body.Message != input.Message {
			t.Errorf("request body mismatch: got %s, want %s", body.Message, input.Message)
		}

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := PutJSON[testPayload, testPayload](context.Background(), client, "/put", input)
	if err != nil {
		t.Fatalf("PutJSON failed: %v", err)
	}

	if result.Message != response.Message {
		t.Errorf("response mismatch: got %s, want %s", result.Message, response.Message)
	}
}

func TestClient_PatchJSON(t *testing.T) {
	input := testPayload{Message: "sending-patch", Status: 1}
	response := testPayload{Message: "received-patch", Status: 2}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected application/json content type")
		}

		var body testPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if body.Message != input.Message {
			t.Errorf("request body mismatch: got %s, want %s", body.Message, input.Message)
		}

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := PatchJSON[testPayload, testPayload](context.Background(), client, "/patch", input)
	if err != nil {
		t.Fatalf("PatchJSON failed: %v", err)
	}

	if result.Message != response.Message {
		t.Errorf("response mismatch: got %s, want %s", result.Message, response.Message)
	}
}

func TestClient_DeleteJSON(t *testing.T) {
	input := testPayload{Message: "deleting", Status: 1}
	response := testPayload{Message: "deleted", Status: 2}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected application/json content type")
		}

		var body testPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if body.Message != input.Message {
			t.Errorf("request body mismatch: got %s, want %s", body.Message, input.Message)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := DeleteJSON[testPayload, testPayload](context.Background(), client, "/delete", input)
	if err != nil {
		t.Fatalf("DeleteJSON failed: %v", err)
	}

	if result.Message != response.Message {
		t.Errorf("response mismatch: got %s, want %s", result.Message, response.Message)
	}
}

func TestClient_DeleteJSON_NilPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		// Check that body is empty
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != io.EOF && err != nil {
			t.Errorf("expected empty body, got error: %v", err)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	_, err := DeleteJSON[*testPayload, any](context.Background(), client, "/delete-nil", nil)
	if err != nil {
		t.Fatalf("DeleteJSON failed: %v", err)
	}
}
