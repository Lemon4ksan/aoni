// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func TestRawClient_Verbs(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Received-Method", r.Method)
		w.Header().Set("X-Received-Query", r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)

	client := aoni.NewClient(option.WithBaseURL(ts.URL))
	raw := client.Raw()
	require.NotNil(t, raw)

	tests := []struct {
		name       string
		call       func(ctx context.Context) (*http.Response, error)
		wantMethod string
	}{
		{
			name: "raw_get",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Get(ctx, "/test", mod.WithQuery("k", "v"))
			},
			wantMethod: http.MethodGet,
		},
		{
			name: "raw_post",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Post(ctx, "/test", mod.WithBodyBytes([]byte("payload")))
			},
			wantMethod: http.MethodPost,
		},
		{
			name: "raw_put",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Put(ctx, "/test")
			},
			wantMethod: http.MethodPut,
		},
		{
			name: "raw_patch",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Patch(ctx, "/test")
			},
			wantMethod: http.MethodPatch,
		},
		{
			name: "raw_delete",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Delete(ctx, "/test")
			},
			wantMethod: http.MethodDelete,
		},
		{
			name: "raw_head",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Head(ctx, "/test")
			},
			wantMethod: http.MethodHead,
		},
		{
			name: "raw_options",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Options(ctx, "/test")
			},
			wantMethod: http.MethodOptions,
		},
		{
			name: "raw_trace",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Trace(ctx, "/test")
			},
			wantMethod: http.MethodTrace,
		},
		{
			name: "raw_connect",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Connect(ctx, "/test")
			},
			wantMethod: http.MethodConnect,
		},
		{
			name: "raw_arbitrary_request",
			call: func(ctx context.Context) (*http.Response, error) {
				return raw.Request(ctx, "CUSTOM", "/test")
			},
			wantMethod: "CUSTOM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.call(t.Context())
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, tt.wantMethod, resp.Header.Get("X-Received-Method"))
		})
	}
}
