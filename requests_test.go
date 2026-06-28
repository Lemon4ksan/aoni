// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reqTestPayload struct {
	Message string `json:"message" validate:"required"`
	Status  int    `json:"status"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Details string `json:"details"`
}

type mockBaseResponse struct {
	Success  bool  `json:"success"`
	Data     any   `json:"data"`
	ErrorVal error `json:"-"`
}

func (b *mockBaseResponse) IsSuccess() bool { return b.Success }
func (b *mockBaseResponse) Error() error    { return b.ErrorVal }
func (b *mockBaseResponse) SetData(d any)   { b.Data = d }

type mockBaseProvider struct {
	Requester
	provider func() BaseResponse
}

func (m *mockBaseProvider) BaseResponse() BaseResponse {
	return m.provider()
}

func setupTestReqServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)

	return server, c
}

func TestClient_GetJSON(t *testing.T) {
	t.Parallel()

	expected := reqTestPayload{Message: "hello", Status: http.StatusOK}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	})

	result, err := GetJSON[reqTestPayload](t.Context(), client, "/json")
	require.NoError(t, err)

	assert.Equal(t, expected.Message, result.Message)
	assert.Equal(t, expected.Status, result.Status)
}

func TestClient_GetJSONEx(t *testing.T) {
	t.Parallel()

	expected := reqTestPayload{Message: "hello_ex", Status: http.StatusOK}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	})

	result, raw, err := GetJSONEx[reqTestPayload](t.Context(), client, "/json_ex")
	require.NoError(t, err)
	require.NotNil(t, raw)

	assert.Equal(t, expected.Message, result.Message)
	assert.Equal(t, http.StatusOK, raw.StatusCode)
}

func TestClient_PostJSON(t *testing.T) {
	t.Parallel()

	input := reqTestPayload{Message: "sending", Status: 1}
	response := reqTestPayload{Message: "received", Status: 2}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body reqTestPayload

		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := PostJSON[reqTestPayload](t.Context(), client, "/post", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_PutJSON(t *testing.T) {
	t.Parallel()

	input := reqTestPayload{Message: "sending-put", Status: 1}
	response := reqTestPayload{Message: "received-put", Status: 2}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body reqTestPayload

		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := PutJSON[reqTestPayload](t.Context(), client, "/put", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_PatchJSON(t *testing.T) {
	t.Parallel()

	input := reqTestPayload{Message: "sending-patch", Status: 1}
	response := reqTestPayload{Message: "received-patch", Status: 2}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body reqTestPayload

		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := PatchJSON[reqTestPayload](t.Context(), client, "/patch", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_DeleteJSON(t *testing.T) {
	t.Parallel()

	input := reqTestPayload{Message: "deleting", Status: 1}
	response := reqTestPayload{Message: "deleted", Status: 2}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body reqTestPayload

		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	})

	result, err := DeleteJSON[reqTestPayload](t.Context(), client, "/delete", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_DeleteJSON_NilPayload(t *testing.T) {
	t.Parallel()
	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)

		var body map[string]any

		err := json.NewDecoder(r.Body).Decode(&body)
		assert.ErrorIs(t, err, io.EOF)

		w.WriteHeader(http.StatusNoContent)
	})

	_, err := DeleteJSON[any](t.Context(), client, "/delete-nil", nil)
	require.NoError(t, err)
}

func TestPostForm(t *testing.T) {
	t.Parallel()

	t.Run("post_form_struct_serialization", func(t *testing.T) {
		t.Parallel()

		input := reqTestPayload{Message: "form-msg", Status: 10}
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
			assert.Equal(t, "form-msg", r.FormValue("message"))
			assert.Equal(t, "10", r.FormValue("status"))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message":"success"}`))
		})

		res, err := PostFormJSON[reqTestPayload](t.Context(), client, "/form", input)
		require.NoError(t, err)
		assert.Equal(t, "success", res.Message)
	})

	t.Run("post_form_io_reader_payload", func(t *testing.T) {
		t.Parallel()

		readerPayload := strings.NewReader("message=direct-reader")
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "direct-reader", r.FormValue("message"))
			w.WriteHeader(http.StatusOK)
		})

		_, err := PostFormJSON[NoResponse](t.Context(), client, "/form-reader", readerPayload)
		require.NoError(t, err)
	})

	t.Run("post_form_validation_failure", func(t *testing.T) {
		t.Parallel()

		// Missing required 'message' field
		invalidInput := reqTestPayload{Status: 10}
		client := NewClient(nil)

		_, err := PostFormJSON[reqTestPayload](t.Context(), client, "/form", invalidInput)
		assert.Error(t, err)
	})
}

func TestClient_UnexpectedHTML_Detection(t *testing.T) {
	t.Parallel()

	t.Run("unexpected_html_error", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><html><body>error page</body></html>"))
		})

		_, err := GetJSON[reqTestPayload](t.Context(), client, "/html")
		assert.ErrorIs(t, err, ErrUnexpectedContentType)
	})

	t.Run("cloudflare_challenge_detected", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(
				[]byte("<html><head><title>Just a moment...</title></head><body>cf-challenge ray id</body></html>"),
			)
		})

		_, err := GetJSON[reqTestPayload](t.Context(), client, "/cloudflare")
		assert.ErrorIs(t, err, ErrCloudflareChallenge)
	})
}

func TestClient_APIError_With_ErrorModel(t *testing.T) {
	t.Parallel()

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"INVALID_AUTH","details":"token expired"}`))
	})

	var errPayload errorPayload

	_, err := GetJSON[reqTestPayload](t.Context(), client, "/error", WithErrorModel(&errPayload))

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)

	extractedModel, ok := apiErr.Model.(*errorPayload)
	require.True(t, ok)
	assert.Equal(t, "INVALID_AUTH", extractedModel.Code)
	assert.Equal(t, "token expired", extractedModel.Details)
}

func TestClient_Diagnostics_SensitiveHeaderRedaction(t *testing.T) {
	t.Parallel()

	var debugOutput bytes.Buffer

	mockLogger := &mockLoggerWriter{out: &debugOutput}

	// Build a client configured to log diagnostics via custom logger
	client := NewClient(nil).WithLogger(mockLogger)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "secret-cookie-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"debug_ok"}`))
	}))
	t.Cleanup(server.Close)

	client = client.WithBaseURL(server.URL)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer sensitive-token-here")

	// Call GetJSON so that handleResponse is executed and diagnostics are triggered
	_, err = GetJSON[testPayload](
		t.Context(),
		client,
		"/debug-test",
		Debug(),
		WithHeader("Authorization", "Bearer sensitive-token-here"),
	)
	require.NoError(t, err)

	outputStr := debugOutput.String()
	assert.Contains(t, outputStr, "authorization: <redacted>")
	assert.Contains(t, outputStr, "set-cookie: <redacted>")
	assert.NotContains(t, outputStr, "sensitive-token-here")
}

func TestClient_BaseResponseProvider(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Corrected JSON structure with 'data' envelope matching mockBaseResponse struct
		_, _ = w.Write([]byte(`{"success":true,"data":{"message":"provider_response"}}`))
	})

	// Wrap original client to behave as a BaseResponseProvider
	providerClient := &mockBaseProvider{
		Requester: client,
		provider: func() BaseResponse {
			return &mockBaseResponse{}
		},
	}

	result, err := GetJSON[testPayload](t.Context(), providerClient, "/provider")
	require.NoError(t, err)
	assert.Equal(t, "provider_response", result.Message)
}

func TestClient_Global_DefaultClient_Wrappers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"global_success"}`))
	}))
	t.Cleanup(server.Close)

	// Temporarily override DefaultClient to point to local server
	oldDefault := DefaultClient
	t.Cleanup(func() { DefaultClient = oldDefault })

	DefaultClient = NewClient(nil).WithBaseURL(server.URL)

	ctx := context.Background()

	// 1. Get
	gRes, err := Get[reqTestPayload](ctx, "/global-get")
	require.NoError(t, err)
	assert.Equal(t, "global_success", gRes.Message)

	// 2. Post
	pRes, err := Post[reqTestPayload](ctx, "/global-post", nil)
	require.NoError(t, err)
	assert.Equal(t, "global_success", pRes.Message)

	// 3. Put
	uRes, err := Put[reqTestPayload](ctx, "/global-put", nil)
	require.NoError(t, err)
	assert.Equal(t, "global_success", uRes.Message)

	// 4. Patch
	hRes, err := Patch[reqTestPayload](ctx, "/global-patch", nil)
	require.NoError(t, err)
	assert.Equal(t, "global_success", hRes.Message)

	// 5. Delete
	dRes, err := Delete[reqTestPayload](ctx, "/global-delete", nil)
	require.NoError(t, err)
	assert.Equal(t, "global_success", dRes.Message)
}

// Helpers for logger mocking inside diagnostic tests.
type mockLoggerWriter struct {
	noopLogger
	out io.Writer
}

func (m *mockLoggerWriter) Debug(msg string, args ...any) {
	for _, arg := range args {
		if s, ok := arg.(string); ok {
			_, _ = m.out.Write([]byte(s))
		}
	}
}
