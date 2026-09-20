// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chaos_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tests/testutil"
)

// -----------------------------------------------------------------------------
// F13: Chaos - Abrupt Socket Termination (TCP RST with Linger 0 & UDP socket drop)
// -----------------------------------------------------------------------------

func TestChaos_F13_TCP_AbruptRST_LingerZero(t *testing.T) {
	defer testutil.Check(t)()

	// Spin up a raw TCP server that abruptly resets connections via SO_LINGER=0
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var activeConns sync.WaitGroup
	defer activeConns.Wait()

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			activeConns.Add(1)
			go func(c net.Conn) {
				defer activeConns.Done()
				// Read headers partially
				buf := make([]byte, 128)
				_, _ = c.Read(buf)

				// Abrupt hard reset: send TCP RST packet to client
				if tcpConn, ok := c.(*net.TCPConn); ok {
					_ = tcpConn.SetLinger(0)
				}
				_ = c.Close()
			}(conn)
		}
	}()

	client := aoni.NewClient(nil)
	defer client.Close()

	// Execute requests against the resetting server
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		resp, reqErr := client.Request(ctx, http.MethodGet, fmt.Sprintf("http://%s/rst-test-%d", ln.Addr().String(), i))
		cancel()

		// Request must fail gracefully with a network reset / EOF error without panic
		assert.Error(t, reqErr)
		if resp != nil {
			_ = resp.Body.Close()
		}
	}
}

func TestChaos_F13_H3_AbruptConnectionDrop(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-h3-chaos"))
	}))

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))

	// Warmup request
	resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/warmup")
	require.NoError(t, err)
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	// Abruptly shut down the H3 server
	_ = server.Close()

	// Subsequent request against dropped server must return error promptly without hanging
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	failResp, failErr := client.Request(ctx, http.MethodGet, server.URL()+"/dropped")
	assert.Error(t, failErr)
	if failResp != nil {
		_ = failResp.Body.Close()
	}

	client.Close()
}

// -----------------------------------------------------------------------------
// F14: Chaos - Mid-Flight Stream Resets & Multiplex Isolation
// -----------------------------------------------------------------------------

func TestChaos_F14_H2_StreamReset_MultiplexIsolation(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH2Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("healthy-stream"))
		case "/reset":
			// Hijack and abruptly reset stream
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Cancel or cause an abrupt stream error
			panic(http.ErrAbortHandler)
		}
	}))
	defer server.Close()

	client := aoni.NewClient(server.Client())
	defer client.Close()

	var wg sync.WaitGroup
	const totalRoutines = 20
	successCount := int64(0)
	var mu sync.Mutex

	for i := 0; i < totalRoutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "/ok"
			if idx%3 == 0 {
				path = "/reset"
			}

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			resp, reqErr := client.Request(ctx, http.MethodGet, server.URL()+path)
			if reqErr == nil {
				b, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if string(b) == "healthy-stream" {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	// Healthy streams must have succeeded despite neighboring streams receiving resets
	assert.True(t, successCount > 0, "healthy multiplexed streams must succeed despite aborts")
}

// -----------------------------------------------------------------------------
// F15: Chaos - Truncated Responses (Premature EOF mid-body)
// -----------------------------------------------------------------------------

func TestChaos_F15_TruncatedBody_UnexpectedEOF(t *testing.T) {
	defer testutil.Check(t)()

	// Raw TCP server that lies about Content-Length then cuts the connection
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var activeConns sync.WaitGroup
	defer activeConns.Wait()

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			activeConns.Add(1)
			go func(c net.Conn) {
				defer activeConns.Done()
				defer c.Close()

				buf := make([]byte, 512)
				_, _ = c.Read(buf)

				// Send headers declaring 10000 bytes, but send only 10 bytes then close
				response := "HTTP/1.1 200 OK\r\nContent-Length: 10000\r\nContent-Type: text/plain\r\n\r\n0123456789"
				_, _ = c.Write([]byte(response))
			}(conn)
		}
	}()

	client := aoni.NewClient(nil)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	resp, reqErr := client.Request(ctx, http.MethodGet, fmt.Sprintf("http://%s/truncated", ln.Addr().String()))
	require.NoError(t, reqErr)
	defer resp.Body.Close()

	// Reading the rest of the body must return an error (io.ErrUnexpectedEOF)
	body, readErr := io.ReadAll(resp.Body)
	assert.True(t, errors.Is(readErr, io.ErrUnexpectedEOF) || readErr != nil, fmt.Sprintf("expected EOF or truncation error, got %v", readErr))
	assert.Equal(t, 10, len(body), "should have read the partial prefix")
}

// -----------------------------------------------------------------------------
// F16: Chaos - Slowloris Read/Write Stalls & Context Deadlines
// -----------------------------------------------------------------------------

func TestChaos_F16_Slowloris_ContextTimeout_NoBlockedGoroutines(t *testing.T) {
	defer testutil.Check(t)()

	// Server that trickles 1 byte every 200ms
	server := testutil.NewH1Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		for i := 0; i < 5; i++ {
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte("x"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	client := aoni.NewClient(nil)
	defer client.Close()

	// Short timeout (50ms) to ensure client context fires well before slowloris completes
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	resp, reqErr := client.Request(ctx, http.MethodGet, server.URL()+"/slowloris")
	if reqErr == nil {
		defer resp.Body.Close()
		_, readErr := io.ReadAll(resp.Body)
		assert.Error(t, readErr)
	} else {
		assert.Error(t, reqErr)
	}
}

// -----------------------------------------------------------------------------
// F17: Inflight State & Resource Cleanup Verification (Turbulence Mixture)
// -----------------------------------------------------------------------------

func TestChaos_F17_TurbulenceMixture_InflightStateCleanup(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH1Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fast":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("fast-response"))
		case "/slow":
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("slow-response"))
		case "/fail":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal-error"))
		}
	}))
	defer server.Close()

	client := aoni.NewClient(nil)
	defer client.Close()

	var wg sync.WaitGroup
	const totalRequests = 100

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			timeout := 200 * time.Millisecond
			if idx%3 == 0 {
				timeout = 10 * time.Millisecond // Will trigger mid-flight cancellation
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			path := "/fast"
			if idx%2 == 0 {
				path = "/slow"
			} else if idx%5 == 0 {
				path = "/fail"
			}

			resp, err := client.Request(ctx, http.MethodGet, server.URL()+path)
			if err == nil {
				_, _ = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()

	// Drain client connections and assert clean state
	client.Close()
}
