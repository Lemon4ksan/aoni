// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package std_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/internal/std"
)

func TestRequest_BasicLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("nil_request_initialization", func(t *testing.T) {
		t.Parallel()

		req := std.NewRequest(nil)

		require.NotNil(t, req)
		defer std.ReleaseRequest(req)

		assert.NotNil(t, req.HTTPRequest())
		assert.NotNil(t, req.Context())
		assert.NotNil(t, req.EngineRequest())
	})

	t.Run("release_nil_request", func(t *testing.T) {
		t.Parallel()
		std.ReleaseRequest(nil)
	})

	t.Run("context_propagation", func(t *testing.T) {
		t.Parallel()
		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/api", nil)
		require.NoError(t, err)

		req := std.NewRequest(httpReq)
		defer std.ReleaseRequest(req)

		type ctxKey struct{}

		ctx := context.WithValue(t.Context(), ctxKey{}, "val")
		req.SetContext(ctx)
		assert.Equal(t, "val", req.Context().Value(ctxKey{}))
	})

	t.Run("method_mutation", func(t *testing.T) {
		t.Parallel()

		req := std.NewRequest(nil)
		defer std.ReleaseRequest(req)

		req.SetMethod(http.MethodPut)
		assert.Equal(t, http.MethodPut, req.Method())

		req.SetMethodBytes([]byte(http.MethodPatch))
		assert.Equal(t, http.MethodPatch, req.Method())
	})
}

func TestRequest_URLAndQueryOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		initialURL string
		mutatePath string
		setKey     string
		setVal     string
		addKey     string
		addVal     string
		wantPath   string
		wantQuery  string
	}{
		{
			name:       "modify_path_and_set_param",
			initialURL: "https://example.com/v1/users",
			mutatePath: "/v2/items",
			setKey:     "filter",
			setVal:     "active",
			wantPath:   "/v2/items",
			wantQuery:  "filter=active",
		},
		{
			name:       "append_param_via_add",
			initialURL: "https://example.com/search?q=golang",
			addKey:     "sort",
			addVal:     "asc",
			wantPath:   "/search",
			wantQuery:  "q=golang&sort=asc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			httpReq, err := http.NewRequest(http.MethodGet, tt.initialURL, nil)
			require.NoError(t, err)

			req := std.NewRequest(httpReq)
			defer std.ReleaseRequest(req)

			if tt.mutatePath != "" {
				req.SetPath(tt.mutatePath)
			}

			if tt.setKey != "" {
				req.SetQueryParam(tt.setKey, tt.setVal)
			}

			if tt.addKey != "" {
				req.AddQueryParam(tt.addKey, tt.addVal)
			}

			assert.Equal(t, tt.wantPath, req.Path())
			assert.Equal(t, tt.wantQuery, req.RawQuery())
		})
	}

	t.Run("set_raw_query_bytes", func(t *testing.T) {
		t.Parallel()

		httpReq, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
		require.NoError(t, err)

		req := std.NewRequest(httpReq)
		defer std.ReleaseRequest(req)

		req.SetRawQueryBytes([]byte("tag=a&tag=b"))
		assert.Equal(t, "tag=a&tag=b", req.RawQuery())
	})

	t.Run("set_full_url_and_uri_bytes", func(t *testing.T) {
		t.Parallel()

		req := std.NewRequest(nil)
		defer std.ReleaseRequest(req)

		req.SetURL("https://custom.target.org/path/resource?foo=bar")
		assert.Equal(t, "/path/resource", req.Path())
		assert.Equal(t, "foo=bar", req.RawQuery())

		req.SetURIBytes([]byte("https://another.org/endpoint"))
		assert.Equal(t, "/endpoint", req.Path())
	})
}

func TestRequest_Headers(t *testing.T) {
	t.Parallel()

	req := std.NewRequest(nil)
	defer std.ReleaseRequest(req)

	t.Run("set_header_and_bytes", func(t *testing.T) {
		req.SetHeader("X-Custom-Header", "Value1")
		assert.Equal(t, "Value1", req.Header("X-Custom-Header"))
		assert.Equal(t, []byte("Value1"), req.HeaderBytes([]byte("X-Custom-Header")))

		req.SetHeaderBytes([]byte("X-Custom-Byte"), []byte("ByteVal"))
		assert.Equal(t, "ByteVal", req.Header("X-Custom-Byte"))
	})

	t.Run("add_header_and_bytes", func(t *testing.T) {
		req.AddHeader("Accept", "application/json")
		req.AddHeaderBytes([]byte("Accept"), []byte("text/plain"))
		assert.Contains(t, req.Header("Accept"), "application/json")
	})

	t.Run("delete_header_and_bytes", func(t *testing.T) {
		req.DelHeader("X-Custom-Header")
		assert.Equal(t, "", req.Header("X-Custom-Header"))

		req.DelHeaderBytes([]byte("X-Custom-Byte"))
		assert.Equal(t, "", req.Header("X-Custom-Byte"))
	})

	t.Run("iterate_headers", func(t *testing.T) {
		req.SetHeader("Header-A", "1")
		req.SetHeader("Header-B", "2")

		collected := make(map[string]string)
		for k, v := range req.Headers() {
			collected[string(k)] = string(v)
		}

		assert.Equal(t, "1", collected["Header-A"])
		assert.Equal(t, "2", collected["Header-B"])
	})

	t.Run("reset_headers", func(t *testing.T) {
		req.ResetHeaders()
		assert.Equal(t, "", req.Header("Header-A"))
		assert.Equal(t, "", req.Header("Header-B"))
	})
}

func TestRequest_BodyOperations(t *testing.T) {
	t.Parallel()

	t.Run("bytes_payload", func(t *testing.T) {
		t.Parallel()

		req := std.NewRequest(nil)
		defer std.ReleaseRequest(req)

		payload := []byte("hello body")
		req.SetBodyBytes(payload)

		assert.Equal(t, payload, req.BodyBytes())

		stream := req.BodyStream()
		require.NotNil(t, stream)
		readBytes, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, payload, readBytes)
	})

	t.Run("stream_payload", func(t *testing.T) {
		t.Parallel()

		req := std.NewRequest(nil)
		defer std.ReleaseRequest(req)

		stream := bytes.NewReader([]byte("streamed data"))
		req.SetBodyStream(stream, int64(len("streamed data")))

		body := req.BodyBytes()
		assert.Equal(t, []byte("streamed data"), body)
	})
}

func TestResponse_Operations(t *testing.T) {
	t.Parallel()

	t.Run("nil_response_initialization", func(t *testing.T) {
		t.Parallel()

		resp := std.NewResponse(nil)

		require.NotNil(t, resp)
		defer std.ReleaseResponse(resp)

		assert.Equal(t, 0, resp.StatusCode())
		assert.Equal(t, "", resp.Status())
		assert.Nil(t, resp.Headers())
		assert.Nil(t, resp.Trailers())
		assert.Nil(t, resp.BodyBytes())
	})

	t.Run("release_nil_response", func(t *testing.T) {
		t.Parallel()
		std.ReleaseResponse(nil)
	})

	t.Run("status_and_header_inspection", func(t *testing.T) {
		t.Parallel()

		httpResp := &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header: http.Header{
				"X-Server": []string{"aoni"},
			},
			Body: io.NopCloser(bytes.NewReader([]byte("not found"))),
		}

		resp := std.NewResponse(httpResp)
		defer std.ReleaseResponse(resp)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode())
		assert.Equal(t, "404 Not Found", resp.Status())
		assert.Equal(t, []byte("404 Not Found"), resp.StatusBytes())
		assert.Equal(t, "aoni", resp.Header("X-Server"))
		assert.Equal(t, []byte("aoni"), resp.HeaderBytes([]byte("X-Server")))

		headers := resp.Headers()
		require.NotNil(t, headers)
		assert.Equal(t, "aoni", headers["X-Server"][0])
	})

	t.Run("trailers_manipulation", func(t *testing.T) {
		t.Parallel()

		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}

		resp := std.NewResponse(httpResp)
		defer std.ReleaseResponse(resp)

		resp.SetTrailers(map[string][]string{
			"X-Checksum": {"sha256-abc"},
		})

		trailers := resp.Trailers()
		require.NotNil(t, trailers)
		assert.Equal(t, "sha256-abc", trailers["X-Checksum"][0])
	})

	t.Run("body_bytes_and_stream", func(t *testing.T) {
		t.Parallel()

		data := []byte("response payload")
		httpResp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(data)),
		}

		resp := std.NewResponse(httpResp)
		defer std.ReleaseResponse(resp)

		assert.Equal(t, data, resp.BodyBytes())
		assert.Equal(t, data, resp.UnsafeBodyBytes())

		stream := resp.BodyStream()
		require.NotNil(t, stream)
		readBytes, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, data, readBytes)

		assert.NotNil(t, resp.HTTPResponse())
		assert.NoError(t, resp.Close())
	})
}

func TestHTTPDoerAdapter(t *testing.T) {
	t.Parallel()

	t.Run("nil_adapter_and_nil_request", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, std.NewHTTPDoerAdapter(nil))

		adapter := std.NewHTTPDoerAdapter(http.DefaultClient)
		require.NotNil(t, adapter)

		_, err := adapter.Do(nil)
		assert.ErrorIs(t, err, std.ErrNilRequest)
	})

	t.Run("successful_round_trip", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("X-Echo-Method", r.Method)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(body)
		}))
		defer server.Close()

		adapter := std.NewHTTPDoerAdapter(server.Client())
		require.NotNil(t, adapter)

		req := std.NewRequest(nil)
		defer std.ReleaseRequest(req)

		req.SetMethod(http.MethodPost)
		req.SetURL(server.URL + "/test")
		req.SetBodyBytes([]byte("echo me"))

		resp, err := adapter.Do(req)
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode())
		assert.Equal(t, http.MethodPost, resp.Header("X-Echo-Method"))
		assert.Equal(t, []byte("echo me"), resp.BodyBytes())
	})

	t.Run("http_doer_func_execution", func(t *testing.T) {
		t.Parallel()

		called := false
		doerFunc := std.HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
			called = true

			return &http.Response{
				StatusCode: http.StatusAccepted,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		})

		adapter := std.NewHTTPDoerAdapter(doerFunc)

		req := std.NewRequest(nil)
		defer std.ReleaseRequest(req)

		resp, err := adapter.Do(req)
		require.NoError(t, err)

		defer resp.Close()

		assert.True(t, called)
		assert.Equal(t, http.StatusAccepted, resp.StatusCode())
	})

	t.Run("underlying_doer_error_propagation", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("network failure")
		doerFunc := std.HTTPDoerFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, expectedErr
		})

		adapter := std.NewHTTPDoerAdapter(doerFunc)

		req := std.NewRequest(nil)
		defer std.ReleaseRequest(req)

		_, err := adapter.Do(req)
		assert.ErrorIs(t, err, expectedErr)
	})
}
