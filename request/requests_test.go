// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package request

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

type testPayload struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

type reqTestPayload struct {
	Message string `json:"message" validate:"required"`
	Status  int    `json:"status"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Details string `json:"details"`
}

func setupTestReqServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *aoni.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	return server, c
}

func TestClient_GetTo(t *testing.T) {
	t.Parallel()

	expected := reqTestPayload{Message: "hello", Status: http.StatusOK}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	})

	result, err := GetTo[reqTestPayload](t.Context(), client, "/json")
	require.NoError(t, err)

	assert.Equal(t, expected.Message, result.Message)
	assert.Equal(t, expected.Status, result.Status)
}

func TestClient_GetToEx(t *testing.T) {
	t.Parallel()

	expected := reqTestPayload{Message: "hello_ex", Status: http.StatusOK}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	})

	result, raw, err := GetToEx[reqTestPayload](t.Context(), client, "/json_ex")
	require.NoError(t, err)
	require.NotNil(t, raw)

	assert.Equal(t, expected.Message, result.Message)
	assert.Equal(t, http.StatusOK, raw.StatusCode)
}

func TestClient_PostTo(t *testing.T) {
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

	result, err := PostTo[reqTestPayload](t.Context(), client, "/post", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_PutTo(t *testing.T) {
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

	result, err := PutTo[reqTestPayload](t.Context(), client, "/put", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_PatchTo(t *testing.T) {
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

	result, err := PatchTo[reqTestPayload](t.Context(), client, "/patch", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_DeleteTo(t *testing.T) {
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

	result, err := DeleteTo[reqTestPayload](t.Context(), client, "/delete", input)
	require.NoError(t, err)
	assert.Equal(t, response.Message, result.Message)
}

func TestClient_DeleteTo_NilPayload(t *testing.T) {
	t.Parallel()
	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)

		var body map[string]any

		err := json.NewDecoder(r.Body).Decode(&body)
		assert.ErrorIs(t, err, io.EOF)

		w.WriteHeader(http.StatusNoContent)
	})

	_, err := DeleteTo[any](t.Context(), client, "/delete-nil", nil)
	require.NoError(t, err)
}

func TestGenericToHelpers(t *testing.T) {
	t.Parallel()

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "xml") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<payload><message>xml-success</message><status>200</status></payload>`))

			return
		}

		if strings.Contains(r.URL.Path, "yaml") {
			w.Header().Set("Content-Type", "application/x-yaml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("message: yaml-success\nstatus: 200\n"))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"to-success","status":200}`))
	})

	type xmlPayload struct {
		XMLName xml.Name `xml:"payload"`
		Message string   `xml:"message"`
		Status  int      `xml:"status"`
	}

	type yamlPayload struct {
		Message string `yaml:"message"`
		Status  int    `yaml:"status"`
	}

	t.Run("GetTo_JSON", func(t *testing.T) {
		res, err := GetTo[reqTestPayload](t.Context(), client, "/get")
		require.NoError(t, err)
		assert.Equal(t, "to-success", res.Message)
	})

	t.Run("PostTo_JSON", func(t *testing.T) {
		res, err := PostTo[reqTestPayload](
			t.Context(),
			client,
			"/post",
			reqTestPayload{Message: "to-msg", Status: 100},
		)
		require.NoError(t, err)
		assert.Equal(t, "to-success", res.Message)
	})

	t.Run("GetTo_XML", func(t *testing.T) {
		res, err := GetTo[xmlPayload](t.Context(), client, "/get-xml", codec.WithXMLDecoder())
		require.NoError(t, err)
		assert.Equal(t, "xml-success", res.Message)
	})

	t.Run("PostTo_XML", func(t *testing.T) {
		body, _ := xml.Marshal(xmlPayload{Message: "xml-input", Status: 10})
		res, err := PostTo[xmlPayload](
			t.Context(), client, "/post-xml",
			strings.NewReader(string(body)),
			mod.WithContentType("application/xml"),
			mod.WithAccept("application/xml"),
			codec.WithXMLDecoder(),
		)
		require.NoError(t, err)
		assert.Equal(t, "xml-success", res.Message)
	})

	t.Run("GetTo_YAML", func(t *testing.T) {
		res, err := GetTo[yamlPayload](t.Context(), client, "/get-yaml", codec.WithYAMLDecoder())
		require.NoError(t, err)
		assert.Equal(t, "yaml-success", res.Message)
	})

	t.Run("PostTo_YAML", func(t *testing.T) {
		res, err := PostTo[yamlPayload](
			t.Context(), client, "/post-yaml",
			strings.NewReader("message: yaml-input\nstatus: 10\n"),
			mod.WithContentType("application/x-yaml"),
			mod.WithAccept("application/x-yaml"),
			codec.WithYAMLDecoder(),
		)
		require.NoError(t, err)
		assert.Equal(t, "yaml-success", res.Message)
	})
}

func TestClient_RawHelpers(t *testing.T) {
	t.Parallel()

	input := reqTestPayload{Message: "raw-msg", Status: 42}

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message":"raw-success"}`))

			return
		}

		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body reqTestPayload

		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, input.Message, body.Message)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"raw-success"}`))
	})

	t.Run("Post raw", func(t *testing.T) {
		resp, err := Post(t.Context(), client, "/post", input)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		bodyBytes, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(bodyBytes), "raw-success")
	})

	t.Run("Patch raw", func(t *testing.T) {
		resp, err := Patch(t.Context(), client, "/patch", input)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Put raw", func(t *testing.T) {
		resp, err := Put(t.Context(), client, "/put", input)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Delete raw", func(t *testing.T) {
		resp, err := Delete(t.Context(), client, "/delete", input)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Get raw", func(t *testing.T) {
		_, clientGet := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusNoContent)
		})
		resp, err := Get(t.Context(), clientGet, "/get")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("ProxyIsolatedCookieJar with WithProxy", func(t *testing.T) {
		proxyURL, err := url.Parse("http://my-proxy-host:8080")
		require.NoError(t, err)

		var capturedProxy string

		clientWithProxy := aoni.NewClient(nil,
			option.WithProxy(proxyURL),
			option.WithBeforeRequest(func(req *http.Request) {
				capturedProxy = cookie.GetProxyAddress(req.Context())
			}),
		)

		_, _ = clientWithProxy.Request(t.Context(), http.MethodGet, "http://localhost:12345")
		assert.Equal(t, "http://my-proxy-host:8080", capturedProxy)
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

		_, err := GetTo[reqTestPayload](t.Context(), client, "/html")
		assert.ErrorIs(t, err, aoni.ErrUnexpectedContentType)
	})

	t.Run("cloudflare_challenge_detected", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(
				[]byte("<html><head><title>Just a moment...</title></head><body>cf-challenge ray id</body></html>"),
			)
		})

		_, err := GetTo[reqTestPayload](t.Context(), client, "/cloudflare")
		assert.ErrorIs(t, err, aoni.ErrCloudflareChallenge)
	})
}

func TestClient_APIError_With_ErrorModel(t *testing.T) {
	t.Parallel()

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"INVALID_AUTH","details":"token expired"}`))
	})

	var errPayload errorPayload

	_, err := GetTo[reqTestPayload](t.Context(), client, "/error", mod.WithErrorModel(&errPayload))

	var apiErr *aoni.APIError
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
	client := aoni.NewClient(nil, option.WithLogger(mockLogger))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "secret-cookie-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"debug_ok"}`))
	}))
	t.Cleanup(server.Close)

	client = client.With(option.WithBaseURL(server.URL))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer sensitive-token-here")

	// Call GetTo so that handleResponse is executed and diagnostics are triggered
	_, err = GetTo[testPayload](
		t.Context(),
		client,
		"/debug-test",
		mod.WithDebug(),
		mod.WithHeader("Authorization", "Bearer sensitive-token-here"),
	)
	require.NoError(t, err)

	outputStr := debugOutput.String()
	assert.Contains(t, outputStr, "authorization: <redacted>")
	assert.Contains(t, outputStr, "set-cookie: <redacted>")
	assert.NotContains(t, outputStr, "sensitive-token-here")
}

// Helpers for logger mocking inside diagnostic tests.
type mockLoggerWriter struct {
	log.DiscardType
	out io.Writer
}

func (m *mockLoggerWriter) Debug(msg string, args ...any) {
	for _, arg := range args {
		if s, ok := arg.(string); ok {
			_, _ = m.out.Write([]byte(s))
		}
	}
}

func TestLimitDecoder_BombPrevention(t *testing.T) {
	t.Parallel()

	type SimpleData struct {
		Field string `json:"field"`
	}

	payload := `{"field": "this data exceeds the safe read limit set on the decoder"}`
	reader := strings.NewReader(payload)

	limited := codec.LimitDecoder(codec.JSONDecoder, 15)

	var target SimpleData

	err := limited.Decode(reader, &target)
	require.Error(t, err, "expected decoder to hit the limits boundary and return error")
}

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	input := "Authorization: Bearer secret-token\r\nCookie: session=abc123\r\nContent-Type: text/plain\r\n"
	result := string(redactHeaders([]byte(input)))

	assert.Contains(t, result, "authorization: <redacted>")
	assert.Contains(t, result, "cookie: <redacted>")
	assert.Contains(t, result, "Content-Type: text/plain")
	assert.NotContains(t, result, "secret-token")
	assert.NotContains(t, result, "session=abc123")
}
