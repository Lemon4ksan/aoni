// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

func TestTransport_KeepAliveConnReuse(t *testing.T) {
	var connCount int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer ts.Close()

	c := fast.NewClient()
	c.Engine().Dial = func(addr string) (net.Conn, error) {
		atomic.AddInt32(&connCount, 1)
		return net.Dial("tcp", addr)
	}

	for range 5 {
		resp, err := c.Request(context.Background(), "GET", ts.URL)
		require.NoError(t, err)
		resp.Close()
	}

	// Connections should be reused, so count should be 1
	assert.Equal(t, int32(1), atomic.LoadInt32(&connCount))
}

func TestTransport_DisableKeepAlives(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()
	c.Engine().MaxIdleConnDuration = -1

	for range 3 {
		resp, err := c.Request(context.Background(), "GET", ts.URL)
		require.NoError(t, err)
		resp.Close()
	}
}

func TestTransport_GzipDecompression(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)

		var buf bytes.Buffer

		gw := gzip.NewWriter(&buf)
		_, _ = gw.Write([]byte("compressed string payload"))
		_ = gw.Close()

		_, _ = w.Write(buf.Bytes())
	}))
	defer ts.Close()

	// Direct fast client
	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "GET", ts.URL)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, "compressed string payload", string(resp.BodyBytes()))
	assert.True(t, resp.Uncompressed())

	// Standard client bridge
	stdClient := fast.NewStdClient(c)
	stdResp, err := stdClient.Get(ts.URL)
	require.NoError(t, err)

	defer stdResp.Body.Close()

	body, err := io.ReadAll(stdResp.Body)
	require.NoError(t, err)
	assert.Equal(t, "compressed string payload", string(body))
	assert.True(t, stdResp.Uncompressed)
}

func TestTransport_CustomDialer(t *testing.T) {
	var dialedAddr string

	dialerCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()
	c.Engine().Dial = func(addr string) (net.Conn, error) {
		dialerCalled = true
		dialedAddr = addr
		return net.Dial("tcp", addr)
	}

	resp, err := c.Request(context.Background(), "GET", ts.URL)
	require.NoError(t, err)
	resp.Close()

	assert.True(t, dialerCalled)
	assert.NotEmpty(t, dialedAddr)
}

func TestTransport_ProxyServer(t *testing.T) {
	var proxyReceivedTarget string

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyReceivedTarget = r.RequestURI

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "proxied ok")
	}))
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)

	c := fast.NewClient(option.WithProxy(proxyURL))
	resp, err := c.Request(context.Background(), "GET", "http://example.com/target-path")
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "proxied ok", string(resp.BodyBytes()))
	assert.NotEmpty(t, proxyReceivedTarget)
}

func TestTransport_ChunkedTransferEncoding(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)

		_, _ = io.WriteString(w, "chunk-1|")

		flusher.Flush()
		time.Sleep(20 * time.Millisecond)

		_, _ = io.WriteString(w, "chunk-2|")

		flusher.Flush()
		time.Sleep(20 * time.Millisecond)

		_, _ = io.WriteString(w, "chunk-3")

		flusher.Flush()
	}))
	defer ts.Close()

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "GET", ts.URL)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, "chunk-1|chunk-2|chunk-3", string(resp.BodyBytes()))
}

func TestTransport_ResponseHeaderTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient(option.WithTimeout(50 * time.Millisecond))
	_, err := c.Request(context.Background(), "GET", ts.URL)
	require.Error(t, err)
}
