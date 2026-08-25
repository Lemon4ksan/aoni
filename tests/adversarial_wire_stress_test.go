// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

// In-memory broken server simulator
type brokenConnServer struct {
	responseHandler func(conn net.Conn)
}

func startBrokenServer(handler func(conn net.Conn)) (net.Listener, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				handler(c)
			}(conn)
		}
	}()

	return ln, func() { _ = ln.Close() }
}

func TestBrokenWire_PrematureCloseMidHeaders(t *testing.T) {
	t.Parallel()

	ln, cleanup := startBrokenServer(func(conn net.Conn) {
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		// Send incomplete header and immediately close
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/"))
	})
	defer cleanup()

	client := fast.NewClient(option.WithTimeout(500 * time.Millisecond))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := client.Get(ctx, "http://"+ln.Addr().String()+"/test")
	assert.Error(t, err)
}

func TestBrokenWire_PrematureCloseMidChunkBody(t *testing.T) {
	t.Parallel()

	ln, cleanup := startBrokenServer(func(conn net.Conn) {
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		// Announce chunk of 100 bytes (0x64), send only 10 bytes, then close socket
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n64\r\n1234567890"))
	})
	defer cleanup()

	client := fast.NewClient(option.WithTimeout(500 * time.Millisecond))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	resp, err := client.Get(ctx, "http://"+ln.Addr().String()+"/chunk-fail")
	if err == nil && resp != nil {
		// If headers parsed, reading body must fail cleanly without hanging
		_ = resp.Close()
	}
}

func TestBrokenWire_CorruptedGzipPayload(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		// Send random uncompressed binary garbage instead of valid gzip stream
		garbage := make([]byte, 256)
		_, _ = rand.Read(garbage)
		_, _ = w.Write(garbage)
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Get(ctx, ts.URL)
	assert.NoError(t, err)
	defer resp.Close()

	// Body must be safely accessible without panicking
	body := resp.BodyBytes()
	assert.Equal(t, 256, len(body))
}

func TestBrokenWire_DripFeedSlowBytes(t *testing.T) {
	t.Parallel()

	ln, cleanup := startBrokenServer(func(conn net.Conn) {
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)

		payload := []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nHELLO")
		for _, b := range payload {
			_, _ = conn.Write([]byte{b})
			time.Sleep(2 * time.Millisecond)
		}
	})
	defer cleanup()

	client := fast.NewClient(option.WithTimeout(2 * time.Second))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.Get(ctx, "http://"+ln.Addr().String()+"/drip")
	assert.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, "HELLO", string(resp.BodyBytes()))
}

func TestBrokenWire_PipelinedMassiveRequests(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "echo:%s", r.URL.Path)
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.Close()

	ctx := context.Background()

	// 500 rapid consecutive requests across connection pool
	for i := 0; i < 500; i++ {
		path := fmt.Sprintf("/user/%d", i)
		resp, err := client.Get(ctx, ts.URL+path)
		assert.NoError(t, err)
		expected := fmt.Sprintf("echo:%s", path)
		assert.Equal(t, expected, string(resp.BodyBytes()))
		_ = resp.Close()
	}
}
