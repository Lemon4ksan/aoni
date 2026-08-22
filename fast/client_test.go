// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

func TestFastClient_BasicGet(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/users", r.URL.Path)
		assert.Equal(t, "test-agent", r.Header.Get("User-Agent"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	client := fast.NewClient(
		option.WithBaseURL(ts.URL),
		option.WithUserAgent("test-agent"),
	)
	defer client.CloseIdleConnections()

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(context.Background())
	req.SetMethod("GET")
	req.SetURL(ts.URL + "/api/v1/users")
	req.SetHeader("User-Agent", "test-agent")

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())

	body := resp.BodyBytes()
	assert.JSONEq(t, `{"status":"ok"}`, string(body))
}

func TestFastClient_PostJSON(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"alice"}`, string(body))

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":123}`))
	}))
	defer ts.Close()

	client := fast.NewClient(option.WithBaseURL(ts.URL))
	defer client.CloseIdleConnections()

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(context.Background())
	req.SetMethod("POST")
	req.SetURL(ts.URL + "/users")
	req.SetHeader("Content-Type", "application/json")
	req.SetBodyBytes([]byte(`{"name":"alice"}`))

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode())
}

func TestFastClient_TimeoutHandling(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := fast.NewClient(option.WithBaseURL(ts.URL))
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetMethod("GET")
	req.SetURL(ts.URL + "/")

	_, err := client.Do(req)
	assert.Error(t, err)
}

func TestFastClient_ContentLengthTruncation(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer ln.Close()

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()

				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\n1234567890"))
			}(conn)
		}
	}()

	client := fast.NewClient(option.WithBaseURL("http://" + ln.Addr().String()))
	defer client.CloseIdleConnections()

	resp, err := client.Request(context.Background(), "GET", "/")
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, "12345", string(resp.BodyBytes()))
}

func TestTargetTracker_Concurrent(t *testing.T) {
	t.Parallel()

	client := fast.NewClient()
	defer client.CloseIdleConnections()

	const (
		workers    = 50
		iterations = 100
	)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				client.TrackHTTPSTarget("example.com:443")
				assert.True(t, client.IsHTTPSTarget("example.com:443"))
				client.UntrackHTTPSTarget("example.com:443")
			}
		}()
	}

	wg.Wait()
}

func TestFastClient_Decompression_Gzip_Brotli_Zstd(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gzip":
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte("uncompressed-gzip-payload"))
			_ = gz.Close()
		case "/br":
			w.Header().Set("Content-Encoding", "br")

			brData := fasthttp.AppendBrotliBytes(nil, []byte("uncompressed-brotli-payload"))
			_, _ = w.Write(brData)
		case "/zstd":
			w.Header().Set("Content-Encoding", "zstd")

			payload := []byte("uncompressed-zstd-payload")
			// Raw single segment zstd frame
			var buf bytes.Buffer
			buf.Write([]byte{0x28, 0xb5, 0x2f, 0xfd, 0x20, byte(len(payload))})
			bh := uint32(1) | (uint32(len(payload)) << 3)
			buf.Write([]byte{byte(bh), byte(bh >> 8), byte(bh >> 16)})
			buf.Write(payload)
			_, _ = w.Write(buf.Bytes())

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := fast.NewClient(option.WithBaseURL(ts.URL))
	defer client.CloseIdleConnections()

	tests := []struct {
		path     string
		expected string
	}{
		{"/gzip", "uncompressed-gzip-payload"},
		{"/br", "uncompressed-brotli-payload"},
		{"/zstd", "uncompressed-zstd-payload"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, err := client.Request(context.Background(), "GET", tt.path)
			require.NoError(t, err)

			defer resp.Close()

			assert.Equal(t, tt.expected, string(resp.BodyBytes()))
		})
	}
}
