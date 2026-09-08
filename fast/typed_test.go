// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type userPayload struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type echoResponse struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body,omitempty"`
}

func newEchoServer(t *testing.T) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)

		if r.URL.Path == "/nocontent" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.URL.Path == "/error" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad_request"}`))

			return
		}

		if r.URL.Path == "/invalid-json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{not-valid-json`))

			return
		}

		resp := echoResponse{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   string(bodyBytes),
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Echo", "echo-ok")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	return ts
}

func TestFastClient_RawVerbs(t *testing.T) {
	t.Parallel()

	ts := newEchoServer(t)
	client := fast.NewClient(option.WithBaseURL(ts.URL))
	t.Cleanup(client.CloseIdleConnections)

	tests := []struct {
		name       string
		execute    func() (fastResponse, error)
		wantMethod string
		wantPath   string
	}{
		{
			name: "raw_get",
			execute: func() (fastResponse, error) {
				return client.Get(t.Context(), "/get-endpoint", mod.WithHeader("X-Req", "1"))
			},
			wantMethod: http.MethodGet,
			wantPath:   "/get-endpoint",
		},
		{
			name: "raw_post",
			execute: func() (fastResponse, error) {
				return client.Post(t.Context(), "/post-endpoint", `{"hello":"world"}`)
			},
			wantMethod: http.MethodPost,
			wantPath:   "/post-endpoint",
		},
		{
			name: "raw_put",
			execute: func() (fastResponse, error) {
				return client.Put(t.Context(), "/put-endpoint", `{"update":true}`)
			},
			wantMethod: http.MethodPut,
			wantPath:   "/put-endpoint",
		},
		{
			name: "raw_patch",
			execute: func() (fastResponse, error) {
				return client.Patch(t.Context(), "/patch-endpoint", `{"patch":1}`)
			},
			wantMethod: http.MethodPatch,
			wantPath:   "/patch-endpoint",
		},
		{
			name: "raw_delete",
			execute: func() (fastResponse, error) {
				return client.Delete(t.Context(), "/delete-endpoint")
			},
			wantMethod: http.MethodDelete,
			wantPath:   "/delete-endpoint",
		},
		{
			name: "raw_fetch_custom",
			execute: func() (fastResponse, error) {
				return client.Fetch(t.Context(), "CUSTOM", "/custom-endpoint", nil)
			},
			wantMethod: "CUSTOM",
			wantPath:   "/custom-endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.execute()
			require.NoError(t, err)

			defer resp.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode())
			assert.Equal(t, "echo-ok", resp.Header("X-Custom-Echo"))

			var echo echoResponse

			err = json.Unmarshal(resp.BodyBytes(), &echo)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMethod, echo.Method)
			assert.Equal(t, tt.wantPath, echo.Path)
		})
	}
}

type fastResponse interface {
	StatusCode() int
	Header(key string) string
	BodyBytes() []byte
	Close() error
}

func TestFastClient_TypedGenerics(t *testing.T) {
	t.Parallel()

	ts := newEchoServer(t)
	client := fast.NewClient(option.WithBaseURL(ts.URL))
	t.Cleanup(client.CloseIdleConnections)

	t.Run("get_to", func(t *testing.T) {
		res, err := client.GetTo[echoResponse](t.Context(), "/typed/get")
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, res.Method)
		assert.Equal(t, "/typed/get", res.Path)
	})

	t.Run("get_into", func(t *testing.T) {
		var res echoResponse

		err := client.GetInto(t.Context(), "/typed/get-into", &res)
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, res.Method)
		assert.Equal(t, "/typed/get-into", res.Path)
	})

	t.Run("post_to", func(t *testing.T) {
		res, err := client.PostTo[echoResponse](t.Context(), "/typed/post", userPayload{ID: 10, Name: "bob"})
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, res.Method)
		assert.Contains(t, res.Body, `"id":10`)
	})

	t.Run("post_into", func(t *testing.T) {
		var res echoResponse

		err := client.PostInto(t.Context(), "/typed/post-into", userPayload{ID: 11, Name: "carol"}, &res)
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, res.Method)
		assert.Contains(t, res.Body, `"id":11`)
	})

	t.Run("put_to_and_put_into", func(t *testing.T) {
		res, err := client.PutTo[echoResponse](t.Context(), "/typed/put", "raw_payload")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPut, res.Method)

		var resInto echoResponse

		err = client.PutInto(t.Context(), "/typed/put-into", "raw_payload", &resInto)
		require.NoError(t, err)
		assert.Equal(t, http.MethodPut, resInto.Method)
	})

	t.Run("patch_to_and_patch_into", func(t *testing.T) {
		res, err := client.PatchTo[echoResponse](t.Context(), "/typed/patch", `{"k":"v"}`)
		require.NoError(t, err)
		assert.Equal(t, http.MethodPatch, res.Method)

		var resInto echoResponse

		err = client.PatchInto(t.Context(), "/typed/patch-into", `{"k":"v"}`, &resInto)
		require.NoError(t, err)
		assert.Equal(t, http.MethodPatch, resInto.Method)
	})

	t.Run("delete_to_and_delete_into", func(t *testing.T) {
		res, err := client.DeleteTo[echoResponse](t.Context(), "/typed/delete")
		require.NoError(t, err)
		assert.Equal(t, http.MethodDelete, res.Method)

		var resInto echoResponse

		err = client.DeleteInto(t.Context(), "/typed/delete-into", &resInto)
		require.NoError(t, err)
		assert.Equal(t, http.MethodDelete, resInto.Method)
	})

	t.Run("fetch_to_and_fetch_into", func(t *testing.T) {
		res, err := client.FetchTo[echoResponse](t.Context(), http.MethodOptions, "/typed/options", nil)
		require.NoError(t, err)
		assert.Equal(t, http.MethodOptions, res.Method)

		var resInto echoResponse

		err = client.FetchInto(t.Context(), http.MethodOptions, "/typed/options-into", nil, &resInto)
		require.NoError(t, err)
		assert.Equal(t, http.MethodOptions, resInto.Method)

		var resDoInto echoResponse

		err = client.DoInto(t.Context(), http.MethodOptions, "/typed/do-into", nil, &resDoInto)
		require.NoError(t, err)
		assert.Equal(t, http.MethodOptions, resDoInto.Method)
	})

	t.Run("invalid_json_response_error", func(t *testing.T) {
		_, err := client.GetTo[echoResponse](t.Context(), "/invalid-json")
		assert.Error(t, err)
	})

	t.Run("no_content_status_204", func(t *testing.T) {
		var res echoResponse

		err := client.GetInto(t.Context(), "/nocontent", &res)
		require.NoError(t, err)
	})
}

func TestFastClient_RequestBuilders(t *testing.T) {
	t.Parallel()

	ts := newEchoServer(t)
	client := fast.NewClient(option.WithBaseURL(ts.URL))
	t.Cleanup(client.CloseIdleConnections)

	t.Run("r_and_new_request_builder", func(t *testing.T) {
		rb := client.R()
		require.NotNil(t, rb)

		rb2 := client.NewRequest()
		require.NotNil(t, rb2)
	})
}

func TestFastResponse_EdgeMethods(t *testing.T) {
	t.Parallel()

	hResp := h1engine.AcquireResponse()
	defer h1engine.ReleaseResponse(hResp)

	hResp.SetStatusCode(http.StatusOK)
	hResp.Header.Set("X-Custom-Echo", "echo-ok")
	hResp.SetBody([]byte(`{"hello":"world"}`))

	fastResp := fast.NewResponse(hResp)
	require.NotNil(t, fastResp)

	t.Run("http_response_conversion", func(t *testing.T) {
		stdResp := fastResp.HTTPResponse()
		require.NotNil(t, stdResp)
		assert.Equal(t, http.StatusOK, stdResp.StatusCode)
		assert.Equal(t, "echo-ok", stdResp.Header.Get("X-Custom-Echo"))
		body, rErr := io.ReadAll(stdResp.Body)
		require.NoError(t, rErr)
		assert.NotEmpty(t, body)
	})

	t.Run("body_stream_reading", func(t *testing.T) {
		stream := fastResp.BodyStream()
		require.NotNil(t, stream)
		body, rErr := io.ReadAll(stream)
		require.NoError(t, rErr)
		assert.NotEmpty(t, body)
		assert.NoError(t, stream.Close())
	})

	t.Run("write_to_buffer", func(t *testing.T) {
		var buf bytes.Buffer

		n, wErr := fastResp.WriteTo(&buf)
		require.NoError(t, wErr)
		assert.True(t, n > 0)
		assert.Equal(t, int64(buf.Len()), n)
	})

	t.Run("uncompressed_flags", func(t *testing.T) {
		assert.False(t, fastResp.Uncompressed())
		fastResp.SetUncompressed(true)
		assert.True(t, fastResp.Uncompressed())
		fastResp.SetUncompressed(false)
	})

	t.Run("engine_responses", func(t *testing.T) {
		assert.NotNil(t, fastResp.FastHTTPResponse())
		assert.NotNil(t, fastResp.EngineResponse())
	})

	t.Run("nil_fast_response_safe", func(t *testing.T) {
		var nilResp *fast.Response
		assert.Nil(t, nilResp.HTTPResponse())
		n, wErr := nilResp.WriteTo(io.Discard)
		assert.NoError(t, wErr)
		assert.Equal(t, int64(0), n)
		nilResp.Release()
	})
}

func TestFastGRPCClient_InvalidType(t *testing.T) {
	t.Parallel()

	client := fast.NewClient()
	t.Cleanup(client.CloseIdleConnections)

	grpcClient := client.GRPC()
	require.NotNil(t, grpcClient)

	// Invoke expecting a non-proto response type should fail immediately with type error
	type nonProto struct {
		Val string
	}

	_, err := grpcClient.Invoke[nonProto](t.Context(), "/test.Service/Method", nil)
	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "proto")
}
