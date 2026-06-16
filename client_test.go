// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"io"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func TestClient_Request_URLConstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/test" {
			t.Errorf("expected path /api/v1/test, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	r, err := client.Request(context.Background(), http.MethodGet, "/api/v1/test")
	if err != nil {
		t.Fatal(err)
	}

	_ = r.Body.Close()
}

func TestClient_Request_GetParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("foo") != "bar" || query.Get("baz") != "123" {
			t.Errorf("unexpected query params: %v", query)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	params := url.Values{}
	params.Set("foo", "bar")
	params.Set("baz", "123")

	r, err := client.Request(context.Background(), http.MethodGet, "/test", WithQuery(params))
	if err != nil {
		t.Fatal(err)
	}

	_ = r.Body.Close()
}

func TestClient_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Default") != "default-val" {
			t.Error("default header missing")
		}

		if r.Header.Get("X-Custom") != "custom-val" {
			t.Error("custom modifier header missing")
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL).WithHeader("X-Default", "default-val")

	mod := func(req *http.Request) {
		req.Header.Set("X-Custom", "custom-val")
	}

	r, err := client.Request(context.Background(), http.MethodGet, "/", mod)
	if err != nil {
		t.Fatal(err)
	}

	_ = r.Body.Close()
}

func TestClient_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "not found"}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	_, err := GetJSON[any](context.Background(), client, "/404")
	if err == nil {
		t.Fatal("expected error on 404 status code, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("expected APIError, got %v", err)
	}

	if !contains(string(apiErr.Body), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}

	if !contains(apiErr.Error(), "404") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	r, err := client.Request(ctx, http.MethodGet, "/")
	if err == nil {
		_ = r.Body.Close()

		t.Fatal("expected error for canceled context, got nil")
	}
}

// Improved generic response for testing
type apiResponse struct {
	Status   string `json:"status"`
	Data     any    `json:"data"`
	ErrorMsg string `json:"error,omitempty"`
}

func (a *apiResponse) IsSuccess() bool  { return a.Status == "success" }
func (a *apiResponse) Error() error     { return errors.New(a.ErrorMsg) }
func (a *apiResponse) SetData(data any) { a.Data = data }

func TestClient_BaseResponse(t *testing.T) {
	t.Run("Success response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "success", "data": {"message": "unwrapped"}}`))
		}))
		defer server.Close()

		client := NewClient(nil).
			WithBaseURL(server.URL).
			WithBaseResponse(func() BaseResponse { return &apiResponse{} })

		result, err := GetJSON[testPayload](context.Background(), client, "/wrapped")
		require.NoError(t, err)
		assert.Equal(t, "unwrapped", result.Message)
	})

	t.Run("Error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "fail", "error": "something went wrong"}`))
		}))
		defer server.Close()

		client := NewClient(nil).
			WithBaseURL(server.URL).
			WithBaseResponse(func() BaseResponse { return &apiResponse{} })

		_, err := GetJSON[testPayload](context.Background(), client, "/error")
		assert.ErrorContains(t, err, "something went wrong")
	})
}

func TestClient_PathTemplates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	t.Run("WithVar single replacement", func(t *testing.T) {
		resp, err := client.Request(
			context.Background(),
			http.MethodGet,
			"/user/{id}/profile",
			WithVar("id", 123),
		)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, "/user/123/profile", resp.Header.Get("X-Path"))
	})

	t.Run("WithVars multiple replacements", func(t *testing.T) {
		resp, err := client.Request(
			context.Background(),
			http.MethodGet,
			"/{group}/{member}",
			WithVars("group", "admins", "member", "bob"),
		)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, "/admins/bob", resp.Header.Get("X-Path"))
	})

	t.Run("Escaping", func(t *testing.T) {
		resp, err := client.Request(
			context.Background(),
			http.MethodGet,
			"/search/{query}",
			WithVar("query", "hello world"),
		)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, "/search/hello%20world", resp.Header.Get("X-Path"))
	})
}

func TestClient_Validation(t *testing.T) {
	type RequiredParams struct {
		ID   int    `url:"id"   validate:"required"`
		Name string `url:"name"`
	}

	type RequiredPayload struct {
		Key string `json:"key" validate:"required"`
	}

	client := NewClient(nil).WithBaseURL("http://localhost")

	t.Run("Missing query param", func(t *testing.T) {
		params := RequiredParams{Name: "test"} // ID is 0 (zero value)
		_, err := GetJSON[any](context.Background(), client, "/test", WithQuery(params))
		assert.Error(t, err)

		var valErr *ValidationError
		if assert.ErrorAs(t, err, &valErr) {
			assert.Equal(t, "ID", valErr.Field)
		}
	})

	t.Run("Missing payload field", func(t *testing.T) {
		payload := RequiredPayload{} // Key is empty
		_, err := PostJSON[RequiredPayload, any](context.Background(), client, "/test", payload)
		assert.Error(t, err)

		var valErr *ValidationError
		if assert.ErrorAs(t, err, &valErr) {
			assert.Equal(t, "Key", valErr.Field)
		}
	})

	t.Run("Validation success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		params := RequiredParams{ID: 1}
		_, err := GetJSON[any](context.Background(), client, "/test", WithQuery(params))
		assert.NoError(t, err)
	})
}

func TestClient_PostForm(t *testing.T) {
	type Params struct {
		ID   int    `url:"id"`
		Name string `url:"name"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		_ = r.ParseForm()
		assert.Equal(t, "123", r.Form.Get("id"))
		assert.Equal(t, "bob", r.Form.Get("name"))

		_, _ = w.Write([]byte(`{"status": 200}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	_, err := PostForm[Params, any](context.Background(), client, "/form", Params{ID: 123, Name: "bob"})
	assert.NoError(t, err)
}

func TestClient_CaptureResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "captured")
		_, _ = w.Write([]byte(`{"message": "ok"}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	var raw *http.Response

	result, err := GetJSON[testPayload](context.Background(), client, "/capture", CaptureResponse(&raw))

	if raw != nil && raw.Body != nil {
		defer raw.Body.Close()
	}

	require.NoError(t, err)
	assert.Equal(t, "ok", result.Message)
	require.NotNil(t, raw)
	assert.Equal(t, "captured", raw.Header.Get("X-Custom-Header"))
	_ = raw.Body.Close()
}

func TestClient_DX_Helpers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Auth
		if r.Header.Get("Authorization") == "Bearer my-token" {
			w.Header().Set("X-Auth", "bearer")
		} else if u, p, ok := r.BasicAuth(); ok && u == "user" && p == "pass" {
			w.Header().Set("X-Auth", "basic")
		}

		// Check UA
		if r.Header.Get("User-Agent") == "G-MAN-BOT" {
			w.Header().Set("X-UA", "ok")
		}

		_, _ = w.Write([]byte(`{"message": "ok"}`))
	}))
	defer server.Close()

	// Update DefaultClient for global helpers test
	DefaultClient = DefaultClient.WithBaseURL(server.URL)

	t.Run("Global Get with Bearer", func(t *testing.T) {
		var raw *http.Response

		res, err := Get[testPayload](context.Background(), "/get", WithBearer("my-token"), CaptureResponse(&raw))
		require.NoError(t, err)

		if raw != nil && raw.Body != nil {
			defer raw.Body.Close()
		}

		assert.Equal(t, "ok", res.Message)
		assert.Equal(t, "bearer", raw.Header.Get("X-Auth"))
	})

	t.Run("Basic Auth and User Agent", func(t *testing.T) {
		var raw *http.Response

		_, err := Get[testPayload](
			context.Background(),
			"/auth",
			WithBasicAuth("user", "pass"),
			WithUserAgent("G-MAN-BOT"),
			CaptureResponse(&raw),
		)

		if raw != nil && raw.Body != nil {
			defer raw.Body.Close()
		}

		require.NoError(t, err)
		assert.Equal(t, "basic", raw.Header.Get("X-Auth"))
		assert.Equal(t, "ok", raw.Header.Get("X-UA"))
	})

	t.Run("Global Put", func(t *testing.T) {
		var raw *http.Response

		_, err := Put[testPayload, testPayload](
			context.Background(),
			"/put",
			testPayload{Message: "put-body"},
			CaptureResponse(&raw),
		)

		if raw != nil && raw.Body != nil {
			defer raw.Body.Close()
		}

		require.NoError(t, err)
		assert.Equal(t, http.MethodPut, raw.Request.Method)
	})

	t.Run("Global Patch", func(t *testing.T) {
		var raw *http.Response

		_, err := Patch[testPayload, testPayload](
			context.Background(),
			"/patch",
			testPayload{Message: "patch-body"},
			CaptureResponse(&raw),
		)

		if raw != nil && raw.Body != nil {
			defer raw.Body.Close()
		}

		require.NoError(t, err)
		assert.Equal(t, http.MethodPatch, raw.Request.Method)
	})

	t.Run("Global Delete", func(t *testing.T) {
		var raw *http.Response

		_, err := Delete[testPayload, testPayload](
			context.Background(),
			"/delete",
			testPayload{Message: "delete-body"},
			CaptureResponse(&raw),
		)

		if raw != nil && raw.Body != nil {
			defer raw.Body.Close()
		}

		require.NoError(t, err)
		assert.Equal(t, http.MethodDelete, raw.Request.Method)
	})

	t.Run("Debug Mode (manual verification)", func(t *testing.T) {
		// Just ensure it doesn't panic
		_, err := Get[testPayload](context.Background(), "/debug", Debug())
		require.NoError(t, err)
	})
}

func TestClient_AdvancedFeatures(t *testing.T) {
	t.Run("Streaming body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			assert.Equal(t, "streamed data", string(body))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		reader := strings.NewReader("streamed data")

		resp, err := client.Request(context.Background(), http.MethodPost, "/", WithBody(reader))
		require.NoError(t, err)

		_ = resp.Body.Close()
	})

	t.Run("Cookies", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("test-cookie")
			if err == nil {
				w.Header().Set("X-Cookie", c.Value)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

		t.Run("WithCookie modifier", func(t *testing.T) {
			resp, err := client.Request(
				context.Background(),
				http.MethodGet,
				"/",
				WithCookie(&http.Cookie{Name: "test-cookie", Value: "yum"}),
			)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, "yum", resp.Header.Get("X-Cookie"))
		})

		t.Run("WithCookies map modifier", func(t *testing.T) {
			resp, err := client.Request(
				context.Background(),
				http.MethodGet,
				"/",
				WithCookies(map[string]string{"test-cookie": "yum-yum"}),
			)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, "yum-yum", resp.Header.Get("X-Cookie"))
		})
	})

	t.Run("Redirect policy", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(w, r, "/end", http.StatusFound)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		t.Run("Disable redirects", func(t *testing.T) {
			client := NewClient(nil).WithBaseURL(server.URL).WithRedirectLimit(0)
			resp, err := client.Request(context.Background(), http.MethodGet, "/start")
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, http.StatusFound, resp.StatusCode) // Should not follow
		})

		t.Run("Limit redirects", func(t *testing.T) {
			// With max 2, it should allow one jump (start -> end)
			client := NewClient(nil).WithBaseURL(server.URL).WithRedirectLimit(2)
			resp, err := client.Request(context.Background(), http.MethodGet, "/start")
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	})

	t.Run("Timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL).WithTimeout(10 * time.Millisecond)
		_, err := client.Request(context.Background(), http.MethodGet, "/")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Client.Timeout exceeded")
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || (len(substr) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr))))
}

func TestClient_WithMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	fields := map[string]string{
		"field1": "val1",
		"field2": "val2",
	}
	files := map[string]io.Reader{
		"file1": strings.NewReader("file content"),
	}

	resp, err := client.Request(context.Background(), http.MethodPost, "/", WithMultipart(fields, files))
	require.NoError(t, err)

	_ = resp.Body.Close()
}

func TestClient_TransportMethod(t *testing.T) {
	client := NewClient(nil)
	tr := client.Transport()
	require.NotNil(t, tr)

	nonStandardClient := NewClient(DoerFunc(func(r *http.Request) (*http.Response, error) {
		return nil, nil
	}))
	assert.Nil(t, nonStandardClient.Transport())
}

type errorStruct struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func TestClient_ErrorModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error": "invalid_grant", "error_description": "expired token"}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	var errModel errorStruct

	_, err := GetJSON[any](context.Background(), client, "/oauth", WithErrorModel(&errModel))
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
	t.Run("Upload progress", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			assert.Equal(t, "1234567890", string(body))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

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
			context.Background(),
			http.MethodPost,
			"/upload",
			WithBody(strings.NewReader("1234567890")),
			WithUploadProgress(uploadProgress),
		)
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.True(t, uploadCalled)
		assert.Equal(t, int64(10), uploadBytes)
	})

	t.Run("Download progress", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("1234567890"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

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
			context.Background(),
			http.MethodGet,
			"/download",
			WithDownloadProgress(downloadProgress),
		)
		require.NoError(t, err)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Equal(t, "1234567890", string(body))
		assert.True(t, downloadCalled)
		assert.Equal(t, int64(10), downloadBytes)
	})
}

func TestClient_AutoTranscoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content encoded in Windows-1251 for "привет" (hello in Russian)
		// "привет" in Windows-1251 is: 0xef 0xf0 0xe8 0xe2 0xe5 0xf2
		w.Header().Set("Content-Type", "application/json; charset=windows-1251")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "` + "\xef\xf0\xe8\xe2\xe5\xf2" + `"}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	result, err := GetJSON[testPayload](context.Background(), client, "/transcode")
	require.NoError(t, err)
	assert.Equal(t, "привет", result.Message)
}

func TestClient_Hedging(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count == 1 {
			// First request sleeps long time
			time.Sleep(200 * time.Millisecond)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "hedged"}`))
	}))
	defer server.Close()

	// Use hedging delay 20ms
	client := NewClient(nil).WithBaseURL(server.URL).WithHedging(20 * time.Millisecond)

	start := time.Now()
	result, err := GetJSON[testPayload](context.Background(), client, "/")
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "hedged", result.Message)
	// We expect the backup request (which runs after 20ms delay) to complete quickly.
	// So total duration should be much less than 200ms.
	assert.Less(t, duration, 150*time.Millisecond)
	assert.GreaterOrEqual(t, atomic.LoadInt32(&requestCount), int32(2))
}

func TestClient_XML_Codecs(t *testing.T) {
	type XMLPayload struct {
		XMLName xml.Name `xml:"payload"`
		Value   string   `xml:"value"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<payload><value>xml-data</value></payload>`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	var result XMLPayload

	resp, err := client.Request(context.Background(), http.MethodGet, "/", AsXML())
	require.NoError(t, err)

	err = XMLDecoder.Decode(resp.Body, &result)
	require.NoError(t, err)
	assert.Equal(t, "xml-data", result.Value)

	_ = resp.Body.Close()
}

func TestClient_GlobalHooks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "hooked", r.Header.Get("X-Before-Hook"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var beforeCalled, afterCalled bool

	client := NewClient(nil).
		WithBaseURL(server.URL).
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

	resp, err := client.Request(context.Background(), http.MethodGet, "/")
	require.NoError(t, err)

	_ = resp.Body.Close()

	assert.True(t, beforeCalled)
	assert.True(t, afterCalled)
}

func TestClient_BOMStripping(t *testing.T) {
	t.Run("UTF-8 BOM stripping", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// UTF-8 BOM followed by valid JSON
			_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"message": "bom-stripped"}`))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		result, err := GetJSON[testPayload](context.Background(), client, "/")
		require.NoError(t, err)
		assert.Equal(t, "bom-stripped", result.Message)
	})

	t.Run("No BOM payload", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message": "no-bom"}`))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		result, err := GetJSON[testPayload](context.Background(), client, "/")
		require.NoError(t, err)
		assert.Equal(t, "no-bom", result.Message)
	})
}

func TestClient_ConnectionPool(t *testing.T) {
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

	// Verify that the original client was not mutated (immutability check)
	origTransport := client.Transport()
	require.NotNil(t, origTransport)
	assert.NotEqual(t, 50, origTransport.MaxIdleConns)

	// CloseIdleConnections should not panic
	tunedClient.CloseIdleConnections()
}

func TestClient_Decompression(t *testing.T) {
	t.Run("Decompress Gzip", func(t *testing.T) {
		var buf bytes.Buffer

		gw := gzip.NewWriter(&buf)
		_, _ = gw.Write([]byte(`{"message": "decompress-gzip"}`))
		_ = gw.Close()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(buf.Bytes())
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		result, err := GetJSON[testPayload](context.Background(), client, "/")
		require.NoError(t, err)
		assert.Equal(t, "decompress-gzip", result.Message)
	})

	t.Run("Decompress Brotli", func(t *testing.T) {
		var buf bytes.Buffer

		bw := brotli.NewWriter(&buf)
		_, _ = bw.Write([]byte(`{"message": "decompress-brotli"}`))
		_ = bw.Close()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "br")
			_, _ = w.Write(buf.Bytes())
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		result, err := GetJSON[testPayload](context.Background(), client, "/")
		require.NoError(t, err)
		assert.Equal(t, "decompress-brotli", result.Message)
	})

	t.Run("Decompress Zstandard", func(t *testing.T) {
		var buf bytes.Buffer

		zw, _ := zstd.NewWriter(&buf)
		_, _ = zw.Write([]byte(`{"message": "decompress-zstd"}`))
		_ = zw.Close()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "zstd")
			_, _ = w.Write(buf.Bytes())
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		result, err := GetJSON[testPayload](context.Background(), client, "/")
		require.NoError(t, err)
		assert.Equal(t, "decompress-zstd", result.Message)
	})
}

func TestClient_ContentTypeGuard(t *testing.T) {
	t.Run("HTML instead of JSON returns ErrUnexpectedContentType", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Hello World</body></html>"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		_, err := GetJSON[testPayload](context.Background(), client, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnexpectedContentType)
		assert.Contains(t, err.Error(), "expected structured data but got HTML")
	})

	t.Run("Cloudflare challenge HTML returns ErrCloudflareChallenge", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>cf-challenge and ray id cloudflare</body></html>"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		_, err := GetJSON[testPayload](context.Background(), client, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCloudflareChallenge)
	})

	t.Run("HTML with RawDecoder succeeds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Hello World</body></html>"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

		var output []byte

		resp, err := client.Request(context.Background(), http.MethodGet, "/", AsRaw())
		require.NoError(t, err)

		defer resp.Body.Close()

		err = RawDecoder.Decode(resp.Body, &output)
		require.NoError(t, err)
		assert.Equal(t, "<html><body>Hello World</body></html>", string(output))
	})
}

func TestClient_TLSFingerprint(t *testing.T) {
	client := NewClient(nil)
	tunedClient := client.WithTLSFingerprint(BrowserChrome)

	tr := tunedClient.Transport()
	require.NotNil(t, tr)
	assert.NotNil(t, tr.DialTLSContext)

	// Immutability check: original client's transport DialTLSContext should be nil
	origTr := client.Transport()
	require.NotNil(t, origTr)
	assert.Nil(t, origTr.DialTLSContext)
}

type mockBodyCloser struct {
	io.Reader
	closed atomic.Bool
}

func (m *mockBodyCloser) Close() error {
	m.closed.Store(true)
	return nil
}

func TestClient_SocketLeakPrevention(t *testing.T) {
	body := &mockBodyCloser{Reader: strings.NewReader("some data")}

	client := NewClient(DoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}))

	// We make a request. We get a response, but we don't close its body, and then we let it go out of scope.
	func() {
		resp, err := client.Request(context.Background(), http.MethodGet, "http://localhost")
		require.NoError(t, err)
		assert.NotNil(t, resp)
		// We do NOT call resp.Body.Close()
	}()

	// Force garbage collection to trigger the finalizer
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
	t.Run("Fails early on Content-Length", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "20")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("01234567890123456789"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL).WithMaxResponseSize(10)
		_, err := client.Request(context.Background(), http.MethodGet, "/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
	})

	t.Run("Fails during read when limit exceeded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Transfer-Encoding", "chunked")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("01234567890123456789"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL).WithMaxResponseSize(10)
		resp, err := client.Request(context.Background(), http.MethodGet, "/")
		require.NoError(t, err)

		defer resp.Body.Close()

		_, err = io.ReadAll(resp.Body)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
	})

	t.Run("Succeeds when under limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("under limit"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL).WithMaxResponseSize(100)
		resp, err := client.Request(context.Background(), http.MethodGet, "/")
		require.NoError(t, err)

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "under limit", string(body))
	})
}

func TestClient_SensitiveHeaderScrubbing(t *testing.T) {
	t.Run("Cross-origin redirect scrubs sensitive headers", func(t *testing.T) {
		var redirectedHeaders http.Header

		// Target server (attacker.com / cross-origin target)
		targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectedHeaders = r.Header

			w.WriteHeader(http.StatusOK)
		}))
		defer targetServer.Close()

		// Original server (api.steam.com)
		origServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, targetServer.URL, http.StatusFound)
		}))
		defer origServer.Close()

		client := NewClient(nil).WithRedirectLimit(3)

		// Set sensitive and insensitive headers
		reqMod := func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer token123")
			req.Header.Set("Cookie", "session=cookie123")
			req.Header.Set("X-Session-ID", "sess123")
			req.Header.Set("X-Access-Token", "tok123")
			req.Header.Set("X-Safe-Header", "keep-me")
		}

		resp, err := client.Request(context.Background(), http.MethodGet, origServer.URL, reqMod)
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Empty(t, redirectedHeaders.Get("Authorization"))
		assert.Empty(t, redirectedHeaders.Get("Cookie"))
		assert.Empty(t, redirectedHeaders.Get("X-Session-ID"))
		assert.Empty(t, redirectedHeaders.Get("X-Access-Token"))
		assert.Equal(t, "keep-me", redirectedHeaders.Get("X-Safe-Header"))
	})

	t.Run("Same-origin redirect preserves sensitive headers", func(t *testing.T) {
		var redirectedHeaders http.Header

		// Same-origin server handling both redirect and final destination
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirect" {
				http.Redirect(w, r, "/target", http.StatusFound)
				return
			}

			redirectedHeaders = r.Header

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithRedirectLimit(3).WithBaseURL(server.URL)

		reqMod := func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer token123")
			req.Header.Set("Cookie", "session=cookie123")
			req.Header.Set("X-Session-ID", "sess123")
			req.Header.Set("X-Safe-Header", "keep-me")
		}

		resp, err := client.Request(context.Background(), http.MethodGet, "/redirect", reqMod)
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Equal(t, "Bearer token123", redirectedHeaders.Get("Authorization"))
		assert.Equal(t, "session=cookie123", redirectedHeaders.Get("Cookie"))
		assert.Equal(t, "sess123", redirectedHeaders.Get("X-Session-ID"))
		assert.Equal(t, "keep-me", redirectedHeaders.Get("X-Safe-Header"))
	})
}

func TestClient_SSRFGuard(t *testing.T) {
	client := NewClient(nil).WithSSRFGuard()

	t.Run("Blocks loopback IPv4", func(t *testing.T) {
		_, err := client.Request(context.Background(), http.MethodGet, "http://127.0.0.1:8080/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSSRFBlocked)
	})

	t.Run("Blocks private network IPv4", func(t *testing.T) {
		_, err := client.Request(context.Background(), http.MethodGet, "http://192.168.1.1:8080/")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSSRFBlocked)
	})
}

func TestClient_HappyEyeballs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithHappyEyeballs(10 * time.Millisecond).WithBaseURL(server.URL)
	resp, err := client.Request(context.Background(), http.MethodGet, "/")
	require.NoError(t, err)

	_ = resp.Body.Close()
}

func TestClient_MultiReadBody(t *testing.T) {
	t.Run("In-memory caching under threshold", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("short body"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL).WithMultiReadBody(100)
		resp, err := client.Request(context.Background(), http.MethodGet, "/")
		require.NoError(t, err)

		defer closeResponse(resp)

		body1, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "short body", string(body1))

		_ = resp.Body.Close()

		body2, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "short body", string(body2))
	})

	t.Run("On-disk caching over threshold", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("long body exceeding threshold"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL).WithMultiReadBody(10)
		resp, err := client.Request(context.Background(), http.MethodGet, "/")
		require.NoError(t, err)

		defer closeResponse(resp)

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
