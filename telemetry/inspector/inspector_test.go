// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inspector_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/telemetry"
	"github.com/lemon4ksan/aoni/telemetry/inspector"
)

func TestTrafficInspector_CaptureAndHistory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Response-Header", "response-val-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil)

	client, ins, err := inspector.Enable(client, "127.0.0.1:0")
	require.NoError(t, err)
	require.NotNil(t, ins)

	t.Cleanup(func() {
		_ = ins.Close()
	})

	resp, err := client.Request(t.Context(), http.MethodPost, server.URL+"/test-path",
		mod.WithHeader("X-Custom-Request-Header", "request-val-987"),
		mod.WithJSONBody(map[string]string{"foo": "bar"}),
	)
	require.NoError(t, err)

	_ = resp.Body.Close()

	requests := ins.GetRequests()
	require.Len(t, requests, 1)

	captured := requests[0]
	assert.Equal(t, http.MethodPost, captured.Method)
	assert.Contains(t, captured.URL, "/test-path")
	assert.Equal(t, http.StatusOK, captured.Status)
	assert.Equal(t, "request-val-987", captured.RequestHeaders["X-Custom-Request-Header"])
	assert.Equal(t, "response-val-123", captured.ResponseHeaders["X-Custom-Response-Header"])
	assert.JSONEq(t, `{"foo":"bar"}`, captured.RequestBody)
}

func TestTrafficInspector_HTTPEndpoints(t *testing.T) {
	t.Parallel()

	insp := inspector.NewTrafficInspector("127.0.0.1:0")
	err := insp.Serve()
	require.NoError(t, err)
	t.Cleanup(func() { _ = insp.Close() })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/api/v1", nil)
	require.NoError(t, err)

	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		ContentLength: 100,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
	}

	traceInfo := &telemetry.TraceInfo{
		RemoteAddr: "93.184.216.34:443",
		DNSLookup:  5 * time.Millisecond,
		TCPConn:    10 * time.Millisecond,
		JA4: &ja4.Report{
			JA4:      "t13d1516h2_8daaf6152771_e5627efa2ab1",
			Protocol: "t",
			Version:  "13",
			SNI:      "d",
		},
	}

	insp.Capture(req, resp, nil, traceInfo)

	reqs := insp.GetRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "http://example.com/api/v1", reqs[0].URL)
	assert.Equal(t, "t13d1516h2_8daaf6152771_e5627efa2ab1", reqs[0].JA4)
}

func TestTrafficInspector_SSEStream(t *testing.T) {
	t.Parallel()

	insp := inspector.NewTrafficInspector("127.0.0.1:0")
	err := insp.Serve()
	require.NoError(t, err)
	t.Cleanup(func() { _ = insp.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/stream-test", nil)
	require.NoError(t, err)

	resp := &http.Response{StatusCode: http.StatusOK, Status: "200 OK"}

	insp.Capture(req, resp, nil, nil)

	reqs := insp.GetRequests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "http://example.com/stream-test", reqs[0].URL)
}

func TestTrafficInspector_LimitHistory(t *testing.T) {
	t.Parallel()

	insp := inspector.NewTrafficInspector("127.0.0.1:0")

	for i := range 550 {
		req, _ := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			fmt.Sprintf("http://example.com/req/%d", i),
			nil,
		)
		resp := &http.Response{StatusCode: http.StatusOK}
		insp.Capture(req, resp, nil, nil)
	}

	reqs := insp.GetRequests()
	assert.Len(t, reqs, 500)
}

func TestMultiInspector_Broadcasting(t *testing.T) {
	t.Parallel()

	insp1 := inspector.NewTrafficInspector("127.0.0.1:0")
	insp2 := inspector.NewTrafficInspector("127.0.0.1:0")

	multi := inspector.NewMultiInspector(insp1, insp2)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/multi", nil)
	resp := &http.Response{StatusCode: http.StatusOK}

	multi.Capture(req, resp, nil, nil)

	assert.Len(t, insp1.GetRequests(), 1)
	assert.Len(t, insp2.GetRequests(), 1)
}
