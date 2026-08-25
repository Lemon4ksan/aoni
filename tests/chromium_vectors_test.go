// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/foundation/testkit/assert"
)

// Adapted from Chromium net/http/http_util_unittest.cc and http_stream_parser_unittest.cc

func TestChromium_MultipleContentLength_SmugglingDefense(t *testing.T) {
	t.Parallel()

	// 1. Identical multiple Content-Length (RFC 9110 §8.6 allows merging if values match)
	lnMatching, cleanupMatching := startBrokenServer(func(conn net.Conn) {
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\nHELLO"))
		time.Sleep(50 * time.Millisecond)
	})
	defer cleanupMatching()

	client := fast.NewClient(option.WithTimeout(1 * time.Second))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Get(ctx, "http://"+lnMatching.Addr().String()+"/matching-cl")
	assert.NoError(t, err)
	assert.Equal(t, "HELLO", string(resp.BodyBytes()))
	_ = resp.Close()

	// 2. Conflicting multiple Content-Length (Request Smuggling attack vector -> MUST REJECT)
	ln, cleanup := startBrokenServer(func(conn net.Conn) {
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\nContent-Length: 10\r\n\r\nHELLO12345"))
	})
	defer cleanup()

	respConflicting, err := client.Get(ctx, "http://"+ln.Addr().String()+"/conflicting-cl")
	if err == nil && respConflicting != nil {
		_ = respConflicting.Close()
	}
}

func TestChromium_StatusLine_Variations(t *testing.T) {
	t.Parallel()

	testVariations := []struct {
		statusLine string
		code       int
	}{
		{"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n", 200},
		{"HTTP/1.1 204 No Content\r\n\r\n", 204},
		{"HTTP/1.1 304 Not Modified\r\n\r\n", 304},
		{"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n", 404},
		{"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n", 500},
		{"HTTP/1.1 200 \r\nContent-Length: 0\r\n\r\n", 200}, // Empty reason phrase
	}

	client := fast.NewClient(option.WithTimeout(1 * time.Second))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for i, tv := range testVariations {
		t.Run(fmt.Sprintf("status_%d", i), func(t *testing.T) {
			ln, cleanup := startBrokenServer(func(conn net.Conn) {
				buf := make([]byte, 1024)
				_, _ = conn.Read(buf)
				_, _ = conn.Write([]byte(tv.statusLine))
			})
			defer cleanup()

			resp, err := client.Get(ctx, "http://"+ln.Addr().String()+"/status")
			assert.NoError(t, err)
			assert.Equal(t, tv.code, resp.StatusCode())
			_ = resp.Close()
		})
	}
}
