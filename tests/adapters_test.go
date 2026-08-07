// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

func TestStdRequest_Contract(t *testing.T) {
	t.Parallel()

	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/foo?a=1", nil)
	require.NoError(t, err)

	req := aoni.NewStdRequest(httpReq)
	require.NotNil(t, req.HTTPRequest())
	assert.Same(t, httpReq, req.EngineRequest())

	// Method
	assert.Equal(t, "GET", req.Method())
	req.SetMethod("POST")
	assert.Equal(t, "POST", req.Method())
	req.SetMethodBytes([]byte("PUT"))
	assert.Equal(t, "PUT", req.Method())

	// Path & URL
	assert.Equal(t, "/foo", req.Path())
	req.SetPath("/bar")
	assert.Equal(t, "/bar", req.Path())

	// Query
	assert.Equal(t, "a=1", req.RawQuery())
	req.AddQueryParam("b", "2")
	assert.Equal(t, "a=1&b=2", req.RawQuery())
	req.SetQueryParam("a", "10")
	assert.Contains(t, req.RawQuery(), "a=10")

	// Headers
	req.SetHeader("X-Test", "val1")
	assert.Equal(t, "val1", req.Header("X-Test"))
	assert.Equal(t, "val1", string(req.HeaderBytes([]byte("X-Test"))))

	req.SetHeaderBytes([]byte("X-Byte-Header"), []byte("byte-val"))
	assert.Equal(t, "byte-val", req.Header("X-Byte-Header"))

	req.AddHeader("X-Multi", "v1")
	req.AddHeaderBytes([]byte("X-Multi"), []byte("v2"))
	assert.Equal(t, "v1", req.Header("X-Multi"))

	req.DelHeader("X-Test")
	assert.Empty(t, req.Header("X-Test"))

	// Body
	req.SetBodyBytes([]byte("hello world"))
	assert.Equal(t, []byte("hello world"), req.BodyBytes())

	// Reset headers
	req.ResetHeaders()
	assert.Empty(t, req.Header("X-Byte-Header"))
}

func TestStdRequest_UnifiedModifiers(t *testing.T) {
	t.Parallel()

	modifiers := []aoni.RequestModifier{
		mod.WithHeader("X-App-ID", "aoni-v1"),
		mod.WithHeaderBytes([]byte("X-Engine"), []byte("std")),
		mod.WithBearer("test-secret-token"),
		mod.WithJSONBody(map[string]string{"foo": "bar"}),
		mod.WithQuery(map[string]string{"page": "1"}),
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", "http://localhost/test", nil)
	require.NoError(t, err)

	stdReq := aoni.NewStdRequest(httpReq)
	for _, m := range modifiers {
		m(stdReq)
	}

	assert.Equal(t, "aoni-v1", stdReq.Header("X-App-ID"))
	assert.Equal(t, "std", stdReq.Header("X-Engine"))
	assert.Equal(t, "Bearer test-secret-token", stdReq.Header("Authorization"))
	assert.Equal(t, "application/json", stdReq.Header("Content-Type"))
	assert.JSONEq(t, `{"foo": "bar"}`, string(stdReq.BodyBytes()))
	assert.Equal(t, "page=1", stdReq.RawQuery())
}

func TestStdResponse_Contract(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rec.Header().Set("X-Resp-Header", "resp-val")
	rec.WriteHeader(http.StatusCreated)
	_, _ = rec.WriteString("std-response-body")

	resp := rec.Result()
	stdResp := aoni.NewStdResponse(resp)
	assert.Equal(t, http.StatusCreated, stdResp.StatusCode())
	assert.Equal(t, "201 Created", stdResp.Status())
	assert.Equal(t, "resp-val", stdResp.Header("X-Resp-Header"))
	assert.Equal(t, []byte("std-response-body"), stdResp.BodyBytes())
	assert.Same(t, resp, stdResp.EngineResponse())
	require.NoError(t, stdResp.Close())
}
