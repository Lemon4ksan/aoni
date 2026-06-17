// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/lemon4ksan/aoni/ja4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

type errorStruct struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type apiResponse struct {
	Status   string `json:"status"`
	Data     any    `json:"data"`
	ErrorMsg string `json:"error,omitempty"`
}

func (a *apiResponse) IsSuccess() bool  { return a.Status == "success" }
func (a *apiResponse) Error() error     { return errors.New(a.ErrorMsg) }
func (a *apiResponse) SetData(data any) { a.Data = data }

type mockBodyCloser struct {
	io.Reader
	closed atomic.Bool
}

func (m *mockBodyCloser) Close() error {
	m.closed.Store(true)
	return nil
}

// setupTestServer creates a test server and pre-configures a client with its URL.
// It registers resource cleanup automatically through t.Cleanup.
func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient(nil).WithBaseURL(server.URL)

	return server, client
}

func TestClient_Request_URLConstruction(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/test" {
			t.Errorf("expected path /api/v1/test, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
	})

	r, err := client.Request(t.Context(), http.MethodGet, "/api/v1/test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Body.Close() })
}

func TestClient_Request_GetParams(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("foo") != "bar" || query.Get("baz") != "123" {
			t.Errorf("unexpected query params: %v", query)
		}

		w.WriteHeader(http.StatusOK)
	})

	params := url.Values{}
	params.Set("foo", "bar")
	params.Set("baz", "123")

	r, err := client.Request(t.Context(), http.MethodGet, "/test", WithQuery(params))
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Body.Close() })
}

func TestClient_Headers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Default") != "default-val" {
			t.Error("default header missing")
		}

		if r.Header.Get("X-Custom") != "custom-val" {
			t.Error("custom modifier header missing")
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewClient(nil).WithBaseURL(server.URL).WithHeader("X-Default", "default-val")

	mod := func(req *http.Request) {
		req.Header.Set("X-Custom", "custom-val")
	}

	r, err := client.Request(t.Context(), http.MethodGet, "/", mod)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Body.Close() })
}

func TestClient_ErrorStatus(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "not found"}`))
	})

	_, err := GetJSON[any](t.Context(), client, "/404")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)

	assert.Contains(t, string(apiErr.Body), "not found")
	assert.Contains(t, apiErr.Error(), "404")
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	r, err := client.Request(ctx, http.MethodGet, "/")
	if err == nil {
		t.Cleanup(func() { _ = r.Body.Close() })
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestClient_BaseResponse(t *testing.T) {
	t.Parallel()

	t.Run("success_response", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "success", "data": {"message": "unwrapped"}}`))
		})

		client = client.WithBaseResponse(func() BaseResponse { return &apiResponse{} })

		result, err := GetJSON[testPayload](t.Context(), client, "/wrapped")
		require.NoError(t, err)
		assert.Equal(t, "unwrapped", result.Message)
	})

	t.Run("error_response", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "fail", "error": "something went wrong"}`))
		})

		client = client.WithBaseResponse(func() BaseResponse { return &apiResponse{} })

		_, err := GetJSON[testPayload](t.Context(), client, "/error")
		assert.ErrorContains(t, err, "something went wrong")
	})
}

func TestClient_PathTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		mods []RequestModifier
		want string
	}{
		{
			name: "with_var_single_replacement",
			path: "/user/{id}/profile",
			mods: []RequestModifier{WithVar("id", 123)},
			want: "/user/123/profile",
		},
		{
			name: "with_vars_multiple_replacements",
			path: "/{group}/{member}",
			mods: []RequestModifier{WithVars("group", "admins", "member", "bob")},
			want: "/admins/bob",
		},
		{
			name: "escaping",
			path: "/search/{query}",
			mods: []RequestModifier{WithVar("query", "hello world")},
			want: "/search/hello%20world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Path", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			})

			resp, err := client.Request(t.Context(), http.MethodGet, tt.path, tt.mods...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, tt.want, resp.Header.Get("X-Path"))
		})
	}
}

func TestClient_Validation(t *testing.T) {
	t.Parallel()

	type RequiredParams struct {
		ID   int    `url:"id"   validate:"required"`
		Name string `url:"name"`
	}

	type RequiredPayload struct {
		Key string `json:"key" validate:"required"`
	}

	client := NewClient(nil).WithBaseURL("http://localhost")

	t.Run("missing_query_param", func(t *testing.T) {
		params := RequiredParams{Name: "test"}
		_, err := GetJSON[any](t.Context(), client, "/test", WithQuery(params))
		require.Error(t, err)

		var valErr *ValidationError
		if assert.ErrorAs(t, err, &valErr) {
			assert.Equal(t, "ID", valErr.Field)
		}
	})

	t.Run("missing_payload_field", func(t *testing.T) {
		payload := RequiredPayload{}
		_, err := PostJSON[RequiredPayload, any](t.Context(), client, "/test", payload)
		require.Error(t, err)

		var valErr *ValidationError
		if assert.ErrorAs(t, err, &valErr) {
			assert.Equal(t, "Key", valErr.Field)
		}
	})

	t.Run("validation_success", func(t *testing.T) {
		_, srvClient := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		params := RequiredParams{ID: 1}
		_, err := GetJSON[any](t.Context(), srvClient, "/test", WithQuery(params))
		assert.NoError(t, err)
	})
}

func TestClient_PostForm(t *testing.T) {
	t.Parallel()

	type Params struct {
		ID   int    `url:"id"`
		Name string `url:"name"`
	}

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		_ = r.ParseForm()
		assert.Equal(t, "123", r.Form.Get("id"))
		assert.Equal(t, "bob", r.Form.Get("name"))

		_, _ = w.Write([]byte(`{"status": 200}`))
	})

	_, err := PostForm[Params, any](t.Context(), client, "/form", Params{ID: 123, Name: "bob"})
	assert.NoError(t, err)
}

func TestClient_CaptureResponse(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "captured")
		_, _ = w.Write([]byte(`{"message": "ok"}`))
	})

	var raw *http.Response

	result, err := GetJSON[testPayload](t.Context(), client, "/capture", CaptureResponse(&raw))
	require.NoError(t, err)

	if raw != nil {
		t.Cleanup(func() { _ = raw.Body.Close() })
	}

	assert.Equal(t, "ok", result.Message)
	require.NotNil(t, raw)
	assert.Equal(t, "captured", raw.Header.Get("X-Custom-Header"))
}

func TestClient_DX_Helpers(t *testing.T) {
	// Sequential execution is required since DefaultClient is globally mutated.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer my-token" {
			w.Header().Set("X-Auth", "bearer")
		} else if u, p, ok := r.BasicAuth(); ok && u == "user" && p == "pass" {
			w.Header().Set("X-Auth", "basic")
		}

		if r.Header.Get("User-Agent") == "G-MAN-BOT" {
			w.Header().Set("X-UA", "ok")
		}

		_, _ = w.Write([]byte(`{"message": "ok"}`))
	}))
	t.Cleanup(server.Close)

	oldDefault := DefaultClient
	t.Cleanup(func() { DefaultClient = oldDefault })

	DefaultClient = DefaultClient.WithBaseURL(server.URL)

	t.Run("global_get_with_bearer", func(t *testing.T) {
		var raw *http.Response

		res, err := Get[testPayload](t.Context(), "/get", WithBearer("my-token"), CaptureResponse(&raw))
		require.NoError(t, err)

		if raw != nil {
			t.Cleanup(func() { _ = raw.Body.Close() })
		}

		assert.Equal(t, "ok", res.Message)
		assert.Equal(t, "bearer", raw.Header.Get("X-Auth"))
	})

	t.Run("basic_auth_and_user_agent", func(t *testing.T) {
		var raw *http.Response

		_, err := Get[testPayload](
			t.Context(),
			"/auth",
			WithBasicAuth("user", "pass"),
			WithUserAgent("G-MAN-BOT"),
			CaptureResponse(&raw),
		)
		require.NoError(t, err)

		if raw != nil {
			t.Cleanup(func() { _ = raw.Body.Close() })
		}

		assert.Equal(t, "basic", raw.Header.Get("X-Auth"))
		assert.Equal(t, "ok", raw.Header.Get("X-UA"))
	})

	t.Run("global_put", func(t *testing.T) {
		var raw *http.Response

		_, err := Put[testPayload, testPayload](
			t.Context(),
			"/put",
			testPayload{Message: "put-body"},
			CaptureResponse(&raw),
		)
		require.NoError(t, err)

		if raw != nil {
			t.Cleanup(func() { _ = raw.Body.Close() })
		}

		assert.Equal(t, http.MethodPut, raw.Request.Method)
	})

	t.Run("global_patch", func(t *testing.T) {
		var raw *http.Response

		_, err := Patch[testPayload, testPayload](
			t.Context(),
			"/patch",
			testPayload{Message: "patch-body"},
			CaptureResponse(&raw),
		)
		require.NoError(t, err)

		if raw != nil {
			t.Cleanup(func() { _ = raw.Body.Close() })
		}

		assert.Equal(t, http.MethodPatch, raw.Request.Method)
	})

	t.Run("global_delete", func(t *testing.T) {
		var raw *http.Response

		_, err := Delete[testPayload, testPayload](
			t.Context(),
			"/delete",
			testPayload{Message: "delete-body"},
			CaptureResponse(&raw),
		)
		require.NoError(t, err)

		if raw != nil {
			t.Cleanup(func() { _ = raw.Body.Close() })
		}

		assert.Equal(t, http.MethodDelete, raw.Request.Method)
	})

	t.Run("debug_mode", func(t *testing.T) {
		_, err := Get[testPayload](t.Context(), "/debug", Debug())
		require.NoError(t, err)
	})
}

func TestClient_AdvancedFeatures(t *testing.T) {
	t.Parallel()

	t.Run("streaming_body", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			assert.Equal(t, "streamed data", string(body))
			w.WriteHeader(http.StatusOK)
		})

		reader := strings.NewReader("streamed data")
		resp, err := client.Request(t.Context(), http.MethodPost, "/", WithBody(reader))
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
	})

	t.Run("cookies", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("test-cookie")
			if err == nil {
				w.Header().Set("X-Cookie", c.Value)
			}

			w.WriteHeader(http.StatusOK)
		})

		t.Run("with_cookie_modifier", func(t *testing.T) {
			resp, err := client.Request(
				t.Context(),
				http.MethodGet,
				"/",
				WithCookie(&http.Cookie{Name: "test-cookie", Value: "yum"}),
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, "yum", resp.Header.Get("X-Cookie"))
		})

		t.Run("with_cookies_map_modifier", func(t *testing.T) {
			resp, err := client.Request(
				t.Context(),
				http.MethodGet,
				"/",
				WithCookies(map[string]string{"test-cookie": "yum-yum"}),
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, "yum-yum", resp.Header.Get("X-Cookie"))
		})
	})

	t.Run("redirect_policy", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(w, r, "/end", http.StatusFound)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		})

		t.Run("disable_redirects", func(t *testing.T) {
			client := client.WithRedirectLimit(0)
			resp, err := client.Request(t.Context(), http.MethodGet, "/start")
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, http.StatusFound, resp.StatusCode)
		})

		t.Run("limit_redirects", func(t *testing.T) {
			client := client.WithRedirectLimit(2)
			resp, err := client.Request(t.Context(), http.MethodGet, "/start")
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	})

	t.Run("timeout", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		})

		client = client.WithTimeout(10 * time.Millisecond)
		_, err := client.Request(t.Context(), http.MethodGet, "/")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Client.Timeout exceeded")
	})
}

func TestClient_WithMultipart(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		err := r.ParseMultipartForm(10 * 1024 * 1024)
		require.NoError(t, err)

		assert.Equal(t, "val1", r.FormValue("field1"))
		assert.Equal(t, "val2", r.FormValue("field2"))

		file, _, err := r.FormFile("file1")
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "file content", string(data))

		w.WriteHeader(http.StatusOK)
	})

	fields := map[string]string{
		"field1": "val1",
		"field2": "val2",
	}
	files := map[string]io.Reader{
		"file1": strings.NewReader("file content"),
	}

	resp, err := client.Request(t.Context(), http.MethodPost, "/", WithMultipart(fields, files))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
}

func TestClient_TransportMethod(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)
	tr := client.Transport()
	require.NotNil(t, tr)

	nonStandardClient := NewClient(DoerFunc(func(r *http.Request) (*http.Response, error) {
		return nil, nil
	}))
	assert.Nil(t, nonStandardClient.Transport())
}

func TestClient_ErrorModel(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error": "invalid_grant", "error_description": "expired token"}`))
	})

	var errModel errorStruct

	_, err := GetJSON[any](t.Context(), client, "/oauth", WithErrorModel(&errModel))
	require.Error(t, err)

	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.NotNil(t, apiErr.Model)

	m, ok := apiErr.Model.(*errorStruct)
	require.True(t, ok)
	assert.Equal(t, "invalid_grant", m.Error)
	assert.Equal(t, "expired token", m.ErrorDescription)
}

func TestClient_ProgressCallbacks(t *testing.T) {
	t.Parallel()

	t.Run("upload_progress", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			assert.Equal(t, "1234567890", string(body))
			w.WriteHeader(http.StatusOK)
		})

		var (
			uploadCalled bool
			uploadBytes  int64
		)

		uploadProgress := func(current, total int64) {
			uploadCalled = true
			uploadBytes = current

			assert.Equal(t, int64(10), total)
		}

		resp, err := client.Request(
			t.Context(),
			http.MethodPost,
			"/upload",
			WithBody(strings.NewReader("1234567890")),
			WithUploadProgress(uploadProgress),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		assert.True(t, uploadCalled)
		assert.Equal(t, int64(10), uploadBytes)
	})

	t.Run("download_progress", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("1234567890"))
		})

		var (
			downloadCalled bool
			downloadBytes  int64
		)

		downloadProgress := func(current, total int64) {
			downloadCalled = true
			downloadBytes = current

			assert.Equal(t, int64(10), total)
		}

		resp, err := client.Request(
			t.Context(),
			http.MethodGet,
			"/download",
			WithDownloadProgress(downloadProgress),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, "1234567890", string(body))
		assert.True(t, downloadCalled)
		assert.Equal(t, int64(10), downloadBytes)
	})
}

func TestClient_AutoTranscoding(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=windows-1251")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "` + "\xef\xf0\xe8\xe2\xe5\xf2" + `"}`))
	})

	result, err := GetJSON[testPayload](t.Context(), client, "/transcode")
	require.NoError(t, err)
	assert.Equal(t, "привет", result.Message)
}

func TestClient_Hedging(t *testing.T) {
	t.Parallel()

	var requestCount int32

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count == 1 {
			time.Sleep(200 * time.Millisecond)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "hedged"}`))
	})

	client = client.WithHedging(20 * time.Millisecond)

	start := time.Now()
	result, err := GetJSON[testPayload](t.Context(), client, "/")
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "hedged", result.Message)
	assert.Less(t, duration, 150*time.Millisecond)
	assert.GreaterOrEqual(t, atomic.LoadInt32(&requestCount), int32(2))
}

func TestClient_XML_Codecs(t *testing.T) {
	t.Parallel()

	type XMLPayload struct {
		XMLName xml.Name `xml:"payload"`
		Value   string   `xml:"value"`
	}

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<payload><value>xml-data</value></payload>`))
	})

	var result XMLPayload

	resp, err := client.Request(t.Context(), http.MethodGet, "/", AsXML())
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	err = XMLDecoder.Decode(resp.Body, &result)
	require.NoError(t, err)
	assert.Equal(t, "xml-data", result.Value)
}

func TestClient_GlobalHooks(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "hooked", r.Header.Get("X-Before-Hook"))
		w.WriteHeader(http.StatusOK)
	})

	var beforeCalled, afterCalled bool

	client = client.
		WithBeforeRequest(func(req *http.Request) {
			beforeCalled = true

			req.Header.Set("X-Before-Hook", "hooked")
		}).
		WithAfterResponse(func(resp *http.Response, err error) {
			afterCalled = true

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.True(t, beforeCalled)
	assert.True(t, afterCalled)
}

func TestClient_BOMStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []byte
		want string
	}{
		{
			name: "utf8_bom_stripping",
			body: []byte("\xEF\xBB\xBF" + `{"message": "bom-stripped"}`),
			want: "bom-stripped",
		},
		{
			name: "no_bom_payload",
			body: []byte(`{"message": "no-bom"}`),
			want: "no-bom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(tt.body)
			})

			result, err := GetJSON[testPayload](t.Context(), client, "/")
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Message)
		})
	}
}

func TestClient_ConnectionPool(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)
	cfg := ConnectionPoolConfig{
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       10 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	tunedClient := client.WithConnectionPool(cfg)
	transport := tunedClient.Transport()
	require.NotNil(t, transport)

	assert.Equal(t, 50, transport.MaxIdleConns)
	assert.Equal(t, 10, transport.MaxIdleConnsPerHost)
	assert.Equal(t, 20, transport.MaxConnsPerHost)
	assert.Equal(t, 10*time.Second, transport.IdleConnTimeout)
	assert.Equal(t, 5*time.Second, transport.ResponseHeaderTimeout)

	origTransport := client.Transport()
	require.NotNil(t, origTransport)
	assert.NotEqual(t, 50, origTransport.MaxIdleConns)

	tunedClient.CloseIdleConnections()
}

func TestClient_Decompression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		compress func(io.Writer) io.WriteCloser
		want     string
	}{
		{
			name:     "decompress_gzip",
			encoding: "gzip",
			compress: func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) },
			want:     "decompress-gzip",
		},
		{
			name:     "decompress_brotli",
			encoding: "br",
			compress: func(w io.Writer) io.WriteCloser { return brotli.NewWriter(w) },
			want:     "decompress-brotli",
		},
		{
			name:     "decompress_zstandard",
			encoding: "zstd",
			compress: func(w io.Writer) io.WriteCloser {
				zw, _ := zstd.NewWriter(w)
				return zw
			},
			want: "decompress-zstd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			w := tt.compress(&buf)
			_, _ = w.Write([]byte(`{"message": "` + tt.want + `"}`))
			_ = w.Close()

			_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Encoding", tt.encoding)
				_, _ = w.Write(buf.Bytes())
			})

			result, err := GetJSON[testPayload](t.Context(), client, "/")
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Message)
		})
	}
}

func TestClient_ContentTypeGuard(t *testing.T) {
	t.Parallel()

	t.Run("html_instead_of_json_returns_error", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Hello World</body></html>"))
		})

		_, err := GetJSON[testPayload](t.Context(), client, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnexpectedContentType)
		assert.Contains(t, err.Error(), "expected structured data but got HTML")
	})

	t.Run("cloudflare_challenge_html_returns_error", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>cf-challenge and ray id cloudflare</body></html>"))
		})

		_, err := GetJSON[testPayload](t.Context(), client, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCloudflareChallenge)
	})

	t.Run("html_with_raw_decoder_succeeds", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Hello World</body></html>"))
		})

		var output []byte

		resp, err := client.Request(t.Context(), http.MethodGet, "/", AsRaw())
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		err = RawDecoder.Decode(resp.Body, &output)
		require.NoError(t, err)
		assert.Equal(t, "<html><body>Hello World</body></html>", string(output))
	})
}

func TestClient_TLSFingerprint(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)
	tunedClient := client.WithTLSFingerprint(BrowserChrome)

	tr := tunedClient.Transport()
	require.NotNil(t, tr)
	assert.NotNil(t, tr.DialTLSContext)

	origTr := client.Transport()
	require.NotNil(t, origTr)
	assert.Nil(t, origTr.DialTLSContext)
}

func TestClient_WithJA4Callback(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	var report ja4.JA4Report

	client := NewClient(nil).
		WithTLSFingerprint(BrowserChrome).
		WithJA4Callback(func(r ja4.JA4Report) {
			called.Store(true)
			report = r
		})

	assert.NotNil(t, client)
	// Verify immutability
	origClient := NewClient(nil)
	origTr := origClient.Transport()
	if origTr != nil {
		assert.Nil(t, origTr.DialTLSContext)
	}

	_ = report
	_ = called.Load()
}

func TestClient_JA4CallbackImmutability(t *testing.T) {
	t.Parallel()

	fn := func(r ja4.JA4Report) {}

	client1 := NewClient(nil).WithJA4Callback(fn)
	client2 := client1.WithTLSFingerprint(BrowserChrome)

	// client2 should have the callback
	assert.NotNil(t, client2.ja4Callback)

	// client1 should also have the callback (Clone copies it)
	assert.NotNil(t, client1.ja4Callback)

	// New client without callback should not have it
	client3 := NewClient(nil)
	assert.Nil(t, client3.ja4Callback)
}

func TestTraceJA4(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create a transport that skips certificate verification and uses the test server's TLS config
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Directly connect to the test server, bypassing uTLS
			tcpConn, err := net.Dial("tcp", addr)
			if err != nil {
				return nil, err
			}
			tlsConn := tls.Client(tcpConn, &tls.Config{
				InsecureSkipVerify: true,
				ServerName:         "127.0.0.1",
			})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				tcpConn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	}

	httpClient := &http.Client{Transport: transport}

	var report *ja4.JA4Report
	client := NewClient(httpClient).
		WithJA4Callback(func(r ja4.JA4Report) {
			report = &r
		})

	info := &TraceInfo{}
	_, err := client.Request(
		context.Background(),
		http.MethodGet,
		server.URL,
		TraceJA4(info),
	)
	require.NoError(t, err)

	// JA4H should always be computed (from request headers)
	require.NotNil(t, info.JA4, "TraceJA4 should populate JA4 report")
	assert.NotEmpty(t, info.JA4.JA4H, "JA4H should be computed from request")
	assert.Regexp(t, `^[a-z]{2}[0-9]{2}[cn][rn][0-9]{2}[a-z0-9]{4}_[a-f0-9]{12}_[a-f0-9]{12}_[a-f0-9]{12}$`, info.JA4.JA4H)

	// JA4 (TLS fingerprint) won't be populated because we bypassed uTLS
	// That's expected - JA4 is only populated when WithTLSFingerprint is used

	_ = report
}

func TestTraceJA4_WithTLSFingerprint(t *testing.T) {
	t.Parallel()

	// Generate a self-signed cert for the test server
	cert, key, err := generateTestCert()
	require.NoError(t, err)

	tlsCert, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	// Use WithTLSFingerprint + WithJA4Callback + TraceJA4
	var callbackReport *ja4.JA4Report
	client := NewClient(server.Client()).
		WithTLSFingerprint(BrowserChrome).
		WithJA4Callback(func(r ja4.JA4Report) {
			callbackReport = &r
		})

	info := &TraceInfo{}
	_, err = client.Request(
		context.Background(),
		http.MethodGet,
		server.URL,
		TraceJA4(info),
	)
	require.NoError(t, err)

	// JA4H should be computed from request headers
	require.NotNil(t, info.JA4, "TraceJA4 should populate JA4 report")
	assert.NotEmpty(t, info.JA4.JA4H, "JA4H should be computed")

	// JA4 (TLS fingerprint) should be populated from the uTLS handshake
	assert.NotEmpty(t, info.JA4.JA4, "JA4 should be populated from TLS handshake")
	assert.Regexp(t, `^t[0-9]{2}[di][0-9]{2}[0-9]{2}[a-z0-9]{2}_[a-f0-9]{12}_[a-f0-9]{12}$`, info.JA4.JA4)

	// Callback should also have been invoked
	require.NotNil(t, callbackReport, "JA4 callback should have been invoked")
	assert.Equal(t, info.JA4.JA4, callbackReport.JA4)
}

func TestComputeJA4HFromRequest(t *testing.T) {
	t.Parallel()

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/path", nil)
	req.Header.Set("Host", "example.com")
	req.Header.Set("User-Agent", "test")
	req.Header.Add("Cookie", "session=abc")
	req.Header.Add("Cookie", "token=xyz")
	req.Header.Set("Referer", "http://referrer.com")
	req.Header.Set("Accept-Language", "en-US")

	result := computeJA4HFromRequest(req)
	assert.Regexp(t, `^ge11cr[0-9]{2}[a-z0-9]{4}_[a-f0-9]{12}_[a-f0-9]{12}_[a-f0-9]{12}$`, result)
}

func TestClient_SocketLeakPrevention(t *testing.T) {
	t.Parallel()

	body := &mockBodyCloser{Reader: strings.NewReader("some data")}

	client := NewClient(DoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}))

	func() {
		resp, err := client.Request(t.Context(), http.MethodGet, "http://localhost")
		require.NoError(t, err)
		assert.NotNil(t, resp)
	}()

	for range 20 {
		runtime.GC()
		time.Sleep(5 * time.Millisecond)

		if body.closed.Load() {
			break
		}
	}

	assert.True(t, body.closed.Load(), "expected body to be closed by GC finalizer")
}

func TestClient_ResponseSizeGuard(t *testing.T) {
	t.Parallel()

	t.Run("fails_early_on_content_length", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "20")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("01234567890123456789"))
		})

		client = client.WithMaxResponseSize(10)
		_, err := client.Request(t.Context(), http.MethodGet, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
	})

	t.Run("fails_during_read_when_limit_exceeded", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Transfer-Encoding", "chunked")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("01234567890123456789"))
		})

		client = client.WithMaxResponseSize(10)
		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		_, err = io.ReadAll(resp.Body)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
	})

	t.Run("succeeds_when_under_limit", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("under limit"))
		})

		client = client.WithMaxResponseSize(100)
		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "under limit", string(body))
	})
}

func TestClient_SensitiveHeaderScrubbing(t *testing.T) {
	t.Parallel()

	t.Run("cross_origin_redirect_scrubs_headers", func(t *testing.T) {
		var redirectedHeaders http.Header

		targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectedHeaders = r.Header

			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(targetServer.Close)

		origServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, targetServer.URL, http.StatusFound)
		}))
		t.Cleanup(origServer.Close)

		client := NewClient(nil).WithRedirectLimit(3)

		reqMod := func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer token123")
			req.Header.Set("Cookie", "session=cookie123")
			req.Header.Set("X-Session-ID", "sess123")
			req.Header.Set("X-Access-Token", "tok123")
			req.Header.Set("X-Safe-Header", "keep-me")
		}

		resp, err := client.Request(t.Context(), http.MethodGet, origServer.URL, reqMod)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		assert.Empty(t, redirectedHeaders.Get("Authorization"))
		assert.Empty(t, redirectedHeaders.Get("Cookie"))
		assert.Empty(t, redirectedHeaders.Get("X-Session-ID"))
		assert.Empty(t, redirectedHeaders.Get("X-Access-Token"))
		assert.Equal(t, "keep-me", redirectedHeaders.Get("X-Safe-Header"))
	})

	t.Run("same_origin_redirect_preserves_headers", func(t *testing.T) {
		var redirectedHeaders http.Header

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirect" {
				http.Redirect(w, r, "/target", http.StatusFound)
				return
			}

			redirectedHeaders = r.Header

			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		client := NewClient(nil).WithRedirectLimit(3).WithBaseURL(server.URL)

		reqMod := func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer token123")
			req.Header.Set("Cookie", "session=cookie123")
			req.Header.Set("X-Session-ID", "sess123")
			req.Header.Set("X-Safe-Header", "keep-me")
		}

		resp, err := client.Request(t.Context(), http.MethodGet, "/redirect", reqMod)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		assert.Equal(t, "Bearer token123", redirectedHeaders.Get("Authorization"))
		assert.Equal(t, "session=cookie123", redirectedHeaders.Get("Cookie"))
		assert.Equal(t, "sess123", redirectedHeaders.Get("X-Session-ID"))
		assert.Equal(t, "keep-me", redirectedHeaders.Get("X-Safe-Header"))
	})
}

func TestClient_SSRFGuard(t *testing.T) {
	t.Parallel()

	client := NewClient(nil).WithSSRFGuard()

	t.Run("blocks_loopback_ipv4", func(t *testing.T) {
		_, err := client.Request(t.Context(), http.MethodGet, "http://127.0.0.1:8080/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSSRFBlocked)
	})

	t.Run("blocks_private_network_ipv4", func(t *testing.T) {
		_, err := client.Request(t.Context(), http.MethodGet, "http://192.168.1.1:8080/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSSRFBlocked)
	})
}

func TestClient_HappyEyeballs(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	client = client.WithHappyEyeballs(10 * time.Millisecond)
	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
}

func TestClient_MultiReadBody(t *testing.T) {
	t.Parallel()

	t.Run("in_memory_caching_under_threshold", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("short body"))
		})

		client = client.WithMultiReadBody(100)
		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)
		t.Cleanup(func() { closeResponse(resp) })

		body1, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "short body", string(body1))

		_ = resp.Body.Close()

		body2, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "short body", string(body2))
	})

	t.Run("on_disk_caching_over_threshold", func(t *testing.T) {
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("long body exceeding threshold"))
		})

		client = client.WithMultiReadBody(10)
		resp, err := client.Request(t.Context(), http.MethodGet, "/")
		require.NoError(t, err)
		t.Cleanup(func() { closeResponse(resp) })

		fBody, ok := resp.Body.(*finalizerReadCloser)
		require.True(t, ok)
		mBody, ok := fBody.ReadCloser.(*multiReadBody)
		require.True(t, ok)
		assert.NotNil(t, mBody.tmpFile)

		body1, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "long body exceeding threshold", string(body1))

		_ = resp.Body.Close()

		body2, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "long body exceeding threshold", string(body2))

		tmpFileName := mBody.tmpFile.Name()

		closeResponse(resp)

		_, err = os.Stat(tmpFileName)
		assert.True(t, os.IsNotExist(err), "expected temp file to be deleted")
	})
}

func generateTestCert() (cert, key []byte, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER := x509.MarshalPKCS1PrivateKey(privateKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}
