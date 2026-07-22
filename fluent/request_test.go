// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package fluent_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/telemetry"
)

type userPayload struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type errorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type queryFilter struct {
	Page  int    `url:"page"`
	Sort  string `url:"sort"`
	Limit int    `url:"limit"`
}

func TestFluent_GetJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		assert.Equal(t, "Bearer secret_token", r.Header.Get("Authorization"))
		assert.Equal(t, "1", r.URL.Query().Get("page"))
		assert.Equal(t, "asc", r.URL.Query().Get("sort"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(userPayload{
			ID:    42,
			Name:  "Alex",
			Email: "alex@example.com",
		})
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	var user userPayload

	resp, err := fluent.R(client).
		SetHeader("Accept", "application/json").
		SetBearerToken("secret_token").
		SetQueryParam("page", "1").
		SetQueryParam("sort", "asc").
		SetResult(&user).
		Get("/")

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 42, user.ID)
	assert.Equal(t, "Alex", user.Name)
	assert.Equal(t, "alex@example.com", user.Email)
}

func TestFluent_PathParamInterpolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/users/42/posts/100", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetPathParam("userID", "42").
		SetPathParam("postID", "100").
		Get("/v1/users/{userID}/posts/{postID}")

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_PostJSON_WithBodyAndErrorModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var reqBody userPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "Bob", reqBody.Name)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorPayload{
			Code:    4001,
			Message: "invalid email format",
		})
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	var (
		resUser userPayload
		errBody errorPayload
	)

	resp, err := fluent.R(client).
		SetBody(userPayload{Name: "Bob"}).
		SetResult(&resUser).
		SetError(&errBody).
		Post("/users")

	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, 4001, errBody.Code)
	assert.Equal(t, "invalid email format", errBody.Message)
	assert.Empty(t, resUser.Name)
}

func TestFluent_QueryStruct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		assert.Equal(t, "desc", r.URL.Query().Get("sort"))
		assert.Equal(t, "50", r.URL.Query().Get("limit"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetQueryStruct(queryFilter{Page: 2, Sort: "desc", Limit: 50}).
		Get("/search")

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_GenericShortcuts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(userPayload{ID: 99, Name: "Charlie"})
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	user, resp, err := fluent.GetJSON[userPayload](t.Context(), client, "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 99, user.ID)
	assert.Equal(t, "Charlie", user.Name)
}

func TestFluent_BasicAuthAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "admin", u)
		assert.Equal(t, "pass123", p)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetBasicAuth("admin", "pass123").
		SetTimeout(5 * time.Second).
		Get("/")

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_CustomMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PROPFIND" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(userPayload{ID: 77, Name: "CustomMethod"})

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).Do("PROPFIND", "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	user, resp, err := fluent.DoJSON[userPayload](t.Context(), client, "PROPFIND", "/", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 77, user.ID)
	assert.Equal(t, "CustomMethod", user.Name)
}

func TestFluent_DownloadFile(t *testing.T) {
	fileContent := []byte("hello file downloader world!")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))
	targetPath := t.TempDir() + "/downloaded/test.bin"

	var progressCalled bool

	resp, err := fluent.R(client).
		SetDownloadProgress(func(current, total int64) {
			progressCalled = true
		}).
		SetOutput(targetPath).
		Get("/file.bin")

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, progressCalled)

	savedData, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, fileContent, savedData)
}

func TestFluent_TLSCertificateInspection(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL), option.WithInsecureSkipVerify())

	info := &telemetry.TraceInfo{}
	resp, err := fluent.R(client).
		SetTrace(info).
		Get("/")

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, info.PeerCertificates)

	summary := info.CertSummary()
	require.NotNil(t, summary)
	assert.NotEmpty(t, summary.Issuer)
	assert.NotEmpty(t, summary.SHA256Pin)
}

func BenchmarkFluent_RequestCreation(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = fluent.R(client).
			SetHeader("X-Test", "bench").
			SetQueryParam("key", "val").
			Get("/")
	}
}
