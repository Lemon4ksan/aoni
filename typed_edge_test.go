// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type sampleUser struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
}

func newTypedEchoServer(t *testing.T) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)

		if r.URL.Path == "/html-error" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Error Page</body></html>"))

			return
		}

		if r.URL.Path == "/api-error" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":400,"message":"bad parameters"}`))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo-Method", r.Method)
		w.Header().Set("X-Echo-Content-Type", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)

		if len(bodyBytes) > 0 {
			_, _ = w.Write(bodyBytes)
		} else {
			_, _ = w.Write([]byte(`{"id":1,"email":"test@example.com"}`))
		}
	}))
	t.Cleanup(ts.Close)

	return ts
}

func TestTyped_ExMethods(t *testing.T) {
	t.Parallel()

	ts := newTypedEchoServer(t)
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	t.Run("get_ex", func(t *testing.T) {
		user, resp, err := client.GetEx[sampleUser](t.Context(), "/get")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 1, user.ID)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, http.MethodGet, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("post_ex", func(t *testing.T) {
		reqUser := sampleUser{ID: 2, Email: "post@example.com"}
		user, resp, err := client.PostEx[sampleUser](t.Context(), "/post", reqUser)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 2, user.ID)
		assert.Equal(t, "post@example.com", user.Email)
		assert.Equal(t, http.MethodPost, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("put_ex", func(t *testing.T) {
		reqUser := sampleUser{ID: 3, Email: "put@example.com"}
		user, resp, err := client.PutEx[sampleUser](t.Context(), "/put", reqUser)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 3, user.ID)
		assert.Equal(t, http.MethodPut, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("patch_ex", func(t *testing.T) {
		reqUser := sampleUser{ID: 4, Email: "patch@example.com"}
		user, resp, err := client.PatchEx[sampleUser](t.Context(), "/patch", reqUser)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 4, user.ID)
		assert.Equal(t, http.MethodPatch, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("delete_to_and_delete_ex", func(t *testing.T) {
		user, err := client.DeleteTo[sampleUser](t.Context(), "/delete")
		require.NoError(t, err)
		assert.Equal(t, 1, user.ID)

		userEx, resp, err := client.DeleteEx[sampleUser](t.Context(), "/delete-ex")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 1, userEx.ID)
		assert.Equal(t, http.MethodDelete, resp.Header.Get("X-Echo-Method"))
	})

	t.Run("fetch_ex_and_do_ex", func(t *testing.T) {
		user, resp, err := client.FetchEx[sampleUser](t.Context(), http.MethodOptions, "/fetch-ex", nil)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 1, user.ID)
		assert.Equal(t, http.MethodOptions, resp.Header.Get("X-Echo-Method"))

		userDo, respDo, err := client.DoEx[sampleUser](t.Context(), http.MethodOptions, "/do-ex", nil)
		require.NoError(t, err)

		defer respDo.Body.Close()

		assert.Equal(t, 1, userDo.ID)
	})
}

func TestTyped_PayloadTypes(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo-Content-Type", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)

		respMap := map[string]any{
			"contentType": r.Header.Get("Content-Type"),
			"body":        string(body),
		}
		_ = json.NewEncoder(w).Encode(respMap)
	}))
	t.Cleanup(ts.Close)

	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	type echoMeta struct {
		ContentType string `json:"contentType"`
		Body        string `json:"body"`
	}

	tests := []struct {
		name            string
		payload         any
		wantContentType string
		bodySubstring   string
	}{
		{
			name:            "url_values_payload",
			payload:         url.Values{"user": {"alice"}, "role": {"admin"}},
			wantContentType: "application/x-www-form-urlencoded",
			bodySubstring:   "user=alice",
		},
		{
			name:            "url_values_pointer_payload",
			payload:         &url.Values{"scope": {"read", "write"}},
			wantContentType: "application/x-www-form-urlencoded",
			bodySubstring:   "scope=read",
		},
		{
			name:            "string_payload",
			payload:         "raw-string-content",
			wantContentType: "",
			bodySubstring:   "raw-string-content",
		},
		{
			name:            "bytes_payload",
			payload:         []byte("raw-bytes-content"),
			wantContentType: "",
			bodySubstring:   "raw-bytes-content",
		},
		{
			name:            "io_reader_payload",
			payload:         strings.NewReader("streamed-body-data"),
			wantContentType: "",
			bodySubstring:   "streamed-body-data",
		},
		{
			name:            "json_struct_payload",
			payload:         sampleUser{ID: 42, Email: "struct@test.com"},
			wantContentType: "application/json",
			bodySubstring:   `"id":42`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := client.PostTo[echoMeta](t.Context(), "/echo", tt.payload)
			require.NoError(t, err)
			assert.Equal(t, tt.wantContentType, meta.ContentType)
			assert.Contains(t, meta.Body, tt.bodySubstring)
		})
	}

	t.Run("modifier_as_body_returns_error", func(t *testing.T) {
		t.Parallel()
		_, err := client.PostTo[sampleUser](t.Context(), "/echo", mod.WithHeader("X-Bad", "1"))
		assert.ErrorIs(t, err, aoni.ErrModifierAsBody)
	})
}

func TestTyped_HTMLDetectionAndPeekableReader(t *testing.T) {
	t.Parallel()

	ts := newTypedEchoServer(t)
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	t.Run("html_error_response_detected", func(t *testing.T) {
		t.Parallel()
		_, err := client.GetTo[sampleUser](t.Context(), "/html-error")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected structured data but got HTML")
	})

	t.Run("peekable_reader_resolution", func(t *testing.T) {
		t.Parallel()
		resp, err := client.Raw().Get(t.Context(), "/get")
		require.NoError(t, err)

		defer resp.Body.Close()

		reader1 := aoni.ResolvePeekableReader(resp)
		require.NotNil(t, reader1)

		peekBytes, err := reader1.Peek(5)
		require.NoError(t, err)
		assert.NotEmpty(t, peekBytes)

		reader2 := aoni.ResolvePeekableReader(resp)
		assert.Equal(t, reader1, reader2)
	})
}

func TestTyped_DebugDiagnostics(t *testing.T) {
	t.Parallel()

	ts := newTypedEchoServer(t)
	client := aoni.NewClient(option.WithBaseURL(ts.URL))

	t.Run("debug_request_with_sensitive_headers", func(t *testing.T) {
		t.Parallel()
		user, err := client.PostTo[sampleUser](t.Context(), "/post",
			sampleUser{ID: 99, Email: "debug@test.com"},
			mod.WithDebug(),
			mod.WithHeader("Authorization", "Bearer secret-999"),
			mod.WithHeader("Cookie", "session=top-secret"),
		)
		require.NoError(t, err)
		assert.Equal(t, 99, user.ID)
	})
}
