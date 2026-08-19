// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fluent_test

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/resiliency"
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
	t.Parallel()

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
	t.Cleanup(server.Close)

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

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 42, user.ID)
	assert.Equal(t, "Alex", user.Name)
	assert.Equal(t, "alex@example.com", user.Email)
}

func TestFluent_PathParamInterpolation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/users/42/posts/100", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetPathParam("userID", "42").
		SetPathParam("postID", "100").
		Get("/v1/users/{userID}/posts/{postID}")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_BulkMaps_SetHeaders_SetQueryParams_SetPathParams(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/groups/devs", r.URL.Path)
		assert.Equal(t, "bulk-value", r.Header.Get("X-Bulk-Header"))
		assert.Equal(t, "active", r.URL.Query().Get("status"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetHeaders(map[string]string{"X-Bulk-Header": "bulk-value"}).
		SetQueryParams(map[string]string{"status": "active"}).
		SetPathParams(map[string]string{"group": "devs"}).
		Get("/api/v1/groups/{group}")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_PostJSON_WithBodyAndErrorModel(t *testing.T) {
	t.Parallel()

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
	t.Cleanup(server.Close)

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

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, 4001, errBody.Code)
	assert.Equal(t, "invalid email format", errBody.Message)
	assert.Empty(t, resUser.Name)
}

func TestFluent_QueryStruct(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		assert.Equal(t, "desc", r.URL.Query().Get("sort"))
		assert.Equal(t, "50", r.URL.Query().Get("limit"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetQueryStruct(queryFilter{Page: 2, Sort: "desc", Limit: 50}).
		Get("/search")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_ExpectStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		ExpectStatus(200, 201).
		Post("/created")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	respErr, errFail := fluent.R(client).
		ExpectStatus(200).
		Post("/created")

	require.Error(t, errFail)

	if respErr != nil {
		defer respErr.Body.Close()
	}

	assert.True(t, errors.Is(errFail, fluent.ErrUnexpectedStatus))
	assert.Equal(t, http.StatusCreated, respErr.StatusCode)
}

func TestFluent_Fetch_UniversalGeneric(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 888, "name": "FetchUniversal"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	user, resp, err := fluent.FetchTo[userPayload](t.Context(), client, http.MethodGet, "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 888, user.ID)
	assert.Equal(t, "FetchUniversal", user.Name)
}

func TestFluent_ProtoAndGRPCWebShortcuts(t *testing.T) {
	t.Parallel()

	input := wrapperspb.String("proto_fluent_input")
	response := wrapperspb.String("proto_fluent_response")

	respBytes, err := proto.Marshal(response)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "grpc") || strings.Contains(r.Header.Get("Content-Type"), "grpc-web") ||
			strings.Contains(r.Header.Get("Accept"), "grpc-web") {
			w.Header().Set("Content-Type", "application/grpc-web+proto")

			var frame [5]byte

			frame[0] = 0x00
			binary.BigEndian.PutUint32(frame[1:5], uint32(len(respBytes))) //nolint:gosec
			_, _ = w.Write(frame[:])
			_, _ = w.Write(respBytes)

			return
		}

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBytes)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	t.Run("PostProtoResult", func(t *testing.T) {
		res, resp := fluent.PostProtoResult[wrapperspb.StringValue](t.Context(), client, "/proto-post", input)

		require.NotNil(t, resp)
		defer resp.Body.Close()

		require.True(t, res.IsSuccess())
		val, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "proto_fluent_response", val.GetValue())
	})

	t.Run("GetProtoResult", func(t *testing.T) {
		res, resp := fluent.GetProtoResult[wrapperspb.StringValue](t.Context(), client, "/proto-get")

		require.NotNil(t, resp)
		defer resp.Body.Close()

		require.True(t, res.IsSuccess())
		val, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "proto_fluent_response", val.GetValue())
	})

	t.Run("PostGRPCWebResult", func(t *testing.T) {
		res, resp := fluent.PostGRPCWebResult[wrapperspb.StringValue](t.Context(), client, "/grpc-post", input)

		require.NotNil(t, resp)
		defer resp.Body.Close()

		require.True(t, res.IsSuccess())
		val, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "proto_fluent_response", val.GetValue())
	})

	t.Run("GetGRPCWebResult", func(t *testing.T) {
		res, resp := fluent.GetGRPCWebResult[wrapperspb.StringValue](t.Context(), client, "/grpc-get")

		require.NotNil(t, resp)
		defer resp.Body.Close()

		require.True(t, res.IsSuccess())
		val, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "proto_fluent_response", val.GetValue())
	})
}

func TestFluent_Multipart_SetFormField_And_SetFormFile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(10*1024*1024))
		assert.Equal(t, "John", r.FormValue("username"))

		file, header, err := r.FormFile("avatar")
		require.NoError(t, err)

		defer file.Close()

		assert.Equal(t, "avatar", header.Filename)

		content, _ := io.ReadAll(file)
		assert.Equal(t, "fake_image_bytes", string(content))

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetFormField("username", "John").
		SetFormFile("avatar", strings.NewReader("fake_image_bytes")).
		Post("/upload")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_WithCodec_CustomCodec(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 777, "name": "CodecUser"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	var user userPayload

	resp, err := fluent.R(client).
		WithCodec(codec.JSONCodec, userPayload{Name: "Input"}).
		SetResult(&user).
		Post("/")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 777, user.ID)
	assert.Equal(t, "CodecUser", user.Name)
}

func TestFluent_SetProxy_And_SetRetry(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetProxy("http://127.0.0.1:8080").
		SetRetry(2, 100*time.Millisecond).
		Get("/")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_RetryBuilder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		Retry(resiliency.NewRetry().MaxAttempts(3).OnTransientErrors()).
		Get("/")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_SetOutputFromHeader(t *testing.T) {
	t.Parallel()

	fileContent := []byte("content from header download")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))
	targetDir := t.TempDir()

	resp, err := fluent.R(client).
		SetOutputFromHeader(targetDir).
		Get("/download")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	expectedPath := filepath.Join(targetDir, "report.pdf")
	savedData, readErr := os.ReadFile(expectedPath)
	require.NoError(t, readErr)
	assert.Equal(t, fileContent, savedData)
}

func TestFluent_SetUploadProgress_And_SetCorrelationID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "CORR-12345", r.Header.Get("X-Correlation-ID"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	var uploadCalled bool

	resp, err := fluent.R(client).
		SetCorrelationID("CORR-12345").
		SetUploadProgress(func(_, _ int64) {
			uploadCalled = true
		}).
		SetBody("test upload body").
		Post("/")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, uploadCalled)
}

func TestFluent_Download_HTTPStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))
	targetFile := filepath.Join(t.TempDir(), "missing.bin")

	resp, err := fluent.R(client).
		SetOutput(targetFile).
		Get("/notfound")

	require.Error(t, err)
	require.NotNil(t, resp)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.True(t, errors.Is(err, fluent.ErrDownloadFailed))

	var flErr *fluent.Error
	require.True(t, errors.As(err, &flErr))
	assert.Equal(t, 404, flErr.Code)
	assert.Contains(t, flErr.Error(), "aoni/fluent: download")
}

func TestFluent_Error_Formatting(t *testing.T) {
	t.Parallel()

	e1 := &fluent.Error{Op: "download", Path: "/file", Code: 404, Err: fluent.ErrDownloadFailed}
	assert.Equal(t, "aoni/fluent: download /file status 404: aoni/fluent: download HTTP status error", e1.Error())
	assert.True(t, errors.Is(e1, fluent.ErrDownloadFailed))

	e2 := &fluent.Error{Op: "create_file", Path: "/tmp/test.txt", Err: os.ErrPermission}
	assert.Equal(t, "aoni/fluent: create_file /tmp/test.txt: permission denied", e2.Error())

	e3 := &fluent.Error{Op: "generic_op", Err: errors.New("simple error")}
	assert.Equal(t, "aoni/fluent: generic_op: simple error", e3.Error())

	var nilErr *fluent.Error
	assert.Equal(t, "<nil>", nilErr.Error())
}

func TestFluent_HTTPVerbs_All(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Method))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	r := fluent.R(client)

	resp, err := r.Put("/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	resp, err = fluent.R(client).Patch("/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	resp, err = fluent.R(client).Delete("/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	resp, err = fluent.R(client).Head("/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	resp, err = fluent.R(client).Options("/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestFluent_BasicAuth_DigestAuth_And_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "admin", u)
		assert.Equal(t, "pass123", p)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetBasicAuth("admin", "pass123").
		SetTimeout(5 * time.Second).
		Get("/")

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFluent_DigestAuth_RFC7616(t *testing.T) {
	t.Parallel()

	const (
		username = "admin"
		password = "secretpassword"
		realm    = "TestRealm"
		nonce    = "1234567890abcdef"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="TestRealm", nonce="1234567890abcdef", qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(auth, "Digest ") {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		assert.Contains(t, auth, `username="admin"`)
		assert.Contains(t, auth, `realm="TestRealm"`)
		assert.Contains(t, auth, `nonce="1234567890abcdef"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("digest success!"))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	resp, err := fluent.R(client).
		SetDigestAuth(username, password).
		Get("/")

	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "digest success!", string(body))
}

func TestFluent_Download_Shortcut(t *testing.T) {
	t.Parallel()

	fileContent := []byte("download shortcut content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContent)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))
	targetPath := filepath.Join(t.TempDir(), "download_shortcut.txt")

	resp, err := fluent.R(client).Download(server.URL+"/file", targetPath)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, fileContent, data)
}

func TestFluent_AdditionalMethods(t *testing.T) {
	t.Parallel()

	var (
		capturedMethod      string
		capturedContentType string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedContentType = r.Header.Get("Content-Type")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	t.Run("SetForceJSON", func(t *testing.T) {
		resp, err := fluent.R(client).
			SetForceJSON().
			Get("/")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("SetOutput_and_SetDownloadProgress", func(t *testing.T) {
		var downloadProgressCalled bool

		targetFile := filepath.Join(t.TempDir(), "save_filename.bin")
		resp, err := fluent.R(client).
			SetOutput(targetFile).
			SetDownloadProgress(func(_, _ int64) {
				downloadProgressCalled = true
			}).
			Get("/")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.True(t, downloadProgressCalled)

		content, err := os.ReadFile(targetFile)
		require.NoError(t, err)
		assert.Equal(t, `{"status":"ok"}`, string(content))
	})

	t.Run("SetTrace_and_SetLabel", func(t *testing.T) {
		var traceInfo telemetry.TraceInfo

		resp, err := fluent.R(client).
			SetTrace(&traceInfo).
			SetLabel("route_label").
			Get("/")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "route_label", traceInfo.Label)
	})

	t.Run("Trace_Connect_Execute", func(t *testing.T) {
		resp, err := fluent.R(client).Trace("/")
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Equal(t, http.MethodTrace, capturedMethod)

		resp, err = fluent.R(client).Connect("/")
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Equal(t, http.MethodConnect, capturedMethod)

		resp, err = fluent.R(client).Execute(http.MethodPost, "/")
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Equal(t, http.MethodPost, capturedMethod)
		assert.Equal(t, "", capturedContentType)
	})
}

func TestFluent_ResultMonadicAPI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(userPayload{
			ID:   100,
			Name: "MonadicAlex",
		})
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	t.Run("GetResult_Success", func(t *testing.T) {
		res, resp := fluent.GetResult[userPayload](t.Context(), client, "/")
		require.NotNil(t, resp)
		assert.True(t, res.IsSuccess())
		user, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, 100, user.ID)
		assert.Equal(t, "MonadicAlex", user.Name)
	})

	t.Run("PostResult_Success", func(t *testing.T) {
		res, resp := fluent.PostResult[userPayload](t.Context(), client, "/", userPayload{ID: 100})
		require.NotNil(t, resp)
		assert.True(t, res.IsSuccess())
		user, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "MonadicAlex", user.Name)
	})

	t.Run("PutResult_Success", func(t *testing.T) {
		res, resp := fluent.PutResult[userPayload](t.Context(), client, "/", userPayload{ID: 200})
		require.NotNil(t, resp)
		assert.True(t, res.IsSuccess())
		user, err := res.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "MonadicAlex", user.Name)
	})

	t.Run("DeleteResult_Success", func(t *testing.T) {
		res, resp := fluent.DeleteResult[userPayload](t.Context(), client, "/")
		require.NotNil(t, resp)
		assert.True(t, res.IsSuccess())
	})

	t.Run("PatchResult_Success", func(t *testing.T) {
		res, resp := fluent.PatchResult[userPayload](t.Context(), client, "/", userPayload{ID: 300})
		require.NotNil(t, resp)
		assert.True(t, res.IsSuccess())
	})

	t.Run("FetchResult_ErrorRecover", func(t *testing.T) {
		res, _ := fluent.FetchResult[userPayload](t.Context(), client, http.MethodGet, "/error")
		assert.False(t, res.IsSuccess())
		recovered := res.Recover(func(err error) userPayload {
			return userPayload{ID: 999, Name: "RecoveredUser"}
		})
		assert.Equal(t, 999, recovered.ID)
		assert.Equal(t, "RecoveredUser", recovered.Name)
	})
}

type xmlPayload struct {
	XMLName string `xml:"User"`
	ID      int    `xml:"id"`
	Name    string `xml:"name"`
}

type yamlPayload struct {
	ID   int    `yaml:"id"`
	Name string `yaml:"name"`
}

func TestFluent_XML_And_YAML_Symmetry(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Content-Type"), "xml") || strings.Contains(r.URL.Path, "xml") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<User><id>42</id><name>XMLUser</name></User>`))

			return
		}

		if strings.Contains(r.Header.Get("Content-Type"), "yaml") || strings.Contains(r.URL.Path, "yaml") {
			w.Header().Set("Content-Type", "application/yaml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("id: 43\nname: YAMLUser\n"))

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	t.Run("XML_Builder", func(t *testing.T) {
		var res xmlPayload

		resp, err := fluent.R(client).
			SetXMLBody(xmlPayload{ID: 1, Name: "Out"}).
			SetXMLResult(&res).
			Post("/xml")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 42, res.ID)
		assert.Equal(t, "XMLUser", res.Name)
	})

	t.Run("YAML_Builder", func(t *testing.T) {
		var res yamlPayload

		resp, err := fluent.R(client).
			SetYAMLBody(yamlPayload{ID: 2, Name: "Out"}).
			SetYAMLResult(&res).
			Post("/yaml")
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, 43, res.ID)
		assert.Equal(t, "YAMLUser", res.Name)
	})

	t.Run("PostXMLResult_And_GetXMLResult", func(t *testing.T) {
		resPost, respPost := fluent.PostXMLResult[xmlPayload](t.Context(), client, "/xml", xmlPayload{ID: 10})

		require.NotNil(t, respPost)
		defer respPost.Body.Close()

		require.True(t, resPost.IsSuccess())
		valPost, err := resPost.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, 42, valPost.ID)

		resGet, respGet := fluent.GetXMLResult[xmlPayload](t.Context(), client, "/xml")

		require.NotNil(t, respGet)
		defer respGet.Body.Close()

		require.True(t, resGet.IsSuccess())
		valGet, err := resGet.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "XMLUser", valGet.Name)
	})

	t.Run("PostYAMLResult_And_GetYAMLResult", func(t *testing.T) {
		resPost, respPost := fluent.PostYAMLResult[yamlPayload](t.Context(), client, "/yaml", yamlPayload{ID: 20})

		require.NotNil(t, respPost)
		defer respPost.Body.Close()

		require.True(t, resPost.IsSuccess())
		valPost, err := resPost.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, 43, valPost.ID)

		resGet, respGet := fluent.GetYAMLResult[yamlPayload](t.Context(), client, "/yaml")

		require.NotNil(t, respGet)
		defer respGet.Body.Close()

		require.True(t, resGet.IsSuccess())
		valGet, err := resGet.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "YAMLUser", valGet.Name)
	})
}

func BenchmarkFluent_RequestCreation(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client(), option.WithBaseURL(server.URL))

	b.ReportAllocs()

	for b.Loop() {
		_, _ = fluent.R(client).
			SetHeader("X-Test", "bench").
			SetQueryParam("key", "val").
			Get("/")
	}
}
