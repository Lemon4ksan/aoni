// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
)

func TestRequest_URLAndQueryParams(t *testing.T) {
	fastReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(fastReq)

	req := fast.NewRequest(fastReq)
	req.SetURL("http://example.com/api/v1/search?q=aoni&page=1")

	assert.Equal(t, "GET", req.Method())
	assert.Equal(t, "http://example.com/api/v1/search?q=aoni&page=1", req.URL())
	assert.Equal(t, "/api/v1/search", req.Path())
	assert.Equal(t, "q=aoni&page=1", req.RawQuery())

	req.AddQueryParam("sort", "desc")
	assert.Contains(t, req.RawQuery(), "sort=desc")

	req.SetQueryParam("page", "2")
	assert.Contains(t, req.RawQuery(), "page=2")
	assert.NotContains(t, req.RawQuery(), "page=1")
}

func TestRequest_HeaderMutations(t *testing.T) {
	fastReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(fastReq)

	req := fast.NewRequest(fastReq)
	req.SetHeader("X-Api-Key", "secret-key-123")
	req.SetHeaderBytes([]byte("X-Engine-Name"), []byte("fast-titanium"))

	assert.Equal(t, "secret-key-123", req.Header("X-Api-Key"))
	assert.Equal(t, "fast-titanium", string(req.HeaderBytes([]byte("X-Engine-Name"))))

	req.DelHeader("X-Api-Key")
	assert.Empty(t, req.Header("X-Api-Key"))

	req.ResetHeaders()
	assert.Empty(t, req.Header("X-Engine-Name"))
}

func TestRequest_BodyBytesAndStream(t *testing.T) {
	fastReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(fastReq)

	req := fast.NewRequest(fastReq)

	// Body bytes
	payload := []byte(`{"message":"hello world"}`)
	req.SetBodyBytes(payload)
	assert.Equal(t, payload, req.BodyBytes())

	// Body stream
	streamPayload := bytes.NewReader([]byte("stream-content"))
	req.SetBodyStream(streamPayload, int64(streamPayload.Len()))
	assert.NotNil(t, req.BodyStream())
}

func TestResponse_ContractAndHeaders(t *testing.T) {
	fastResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(fastResp)

	fastResp.SetStatusCode(http.StatusCreated)
	fastResp.Header.Set("Content-Type", "application/json")
	fastResp.Header.Set("X-Request-ID", "req-999")
	fastResp.SetBodyString(`{"id": 42}`)

	resp := fast.NewResponse(fastResp)
	assert.Equal(t, http.StatusCreated, resp.StatusCode())
	assert.Equal(t, "Created", resp.Status())
	assert.Equal(t, "application/json", resp.Header("Content-Type"))
	assert.Equal(t, "req-999", resp.Header("X-Request-ID"))
	assert.Equal(t, `{"id": 42}`, string(resp.BodyBytes()))

	// Close safety
	assert.NoError(t, resp.Close())
}

func TestResponse_RangeRequests(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "bytes=0-4" {
			w.Header().Set("Content-Range", "bytes 0-4/11")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "hello")

			return
		}

		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "GET", ts.URL,
		mod.WithHeader("Range", "bytes=0-4"),
	)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusPartialContent, resp.StatusCode())
	assert.Equal(t, "bytes 0-4/11", resp.Header("Content-Range"))
	assert.Equal(t, "hello", string(resp.BodyBytes()))
}
