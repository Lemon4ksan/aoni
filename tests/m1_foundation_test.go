// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tests/testutil"
)

func newEchoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Proto", r.Proto)
		w.Header().Set("X-Echo-Method", r.Method)
		w.Header().Set("X-Echo-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}

// -----------------------------------------------------------------------------
// F1: Goroutine Leak Detector Tests
// -----------------------------------------------------------------------------

func TestM1_GoroutineLeakDetector_Clean(t *testing.T) {
	defer testutil.Check(t)()

	// Normal in-memory operations with ephemeral goroutines that finish promptly
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
		}()
	}
	wg.Wait()
}

func TestM1_GoroutineLeakDetector_Tolerance(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	// Spawn a background goroutine and assert tolerance allows it
	go func() {
		<-stopCh
	}()

	// Verification with tolerance=1 should pass cleanly
	verify := testutil.VerifyRuntimeGoroutines(t, 1, testutil.WithTimeout(100*time.Millisecond))
	verify()
}

// -----------------------------------------------------------------------------
// F2: In-Process Ephemeral Server Tests (H1, H2, H3)
// -----------------------------------------------------------------------------

func TestM1_H1_InProcessServer_AoniClient(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH1Server(t, newEchoHandler())
	defer server.Close()

	client := aoni.NewClient(nil)
	defer client.Close()

	// 1. GET request
	resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/hello")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, 0, len(body))

	// 2. POST request with body
	postReq, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL()+"/echo",
		bytes.NewReader([]byte("ping-h1-body")),
	)
	require.NoError(t, err)

	postResp, err := client.HTTP().Do(postReq)
	require.NoError(t, err)
	defer postResp.Body.Close()

	assert.Equal(t, http.StatusOK, postResp.StatusCode)
	echoBytes, err := io.ReadAll(postResp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ping-h1-body", string(echoBytes))
}

func TestM1_H2_InProcessServer_AoniClient(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH2Server(t, newEchoHandler())
	defer server.Close()

	// Client wired with H2 TLS config
	client := aoni.NewClient(server.Client())
	defer client.Close()

	// 1. GET request
	resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/h2-test")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "HTTP/2.0", resp.Proto)

	// 2. POST request with body
	postReq, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL()+"/h2-echo",
		bytes.NewReader([]byte("ping-h2-body")),
	)
	require.NoError(t, err)

	postResp, err := client.HTTP().Do(postReq)
	require.NoError(t, err)
	defer postResp.Body.Close()

	assert.Equal(t, http.StatusOK, postResp.StatusCode)
	assert.Equal(t, "HTTP/2.0", postResp.Proto)
	echoBytes, err := io.ReadAll(postResp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ping-h2-body", string(echoBytes))
}

// -----------------------------------------------------------------------------
// F3 & F4: HTTP/3 Engine Integration & QPACK Thread-Safety Tests
// -----------------------------------------------------------------------------

func TestM1_H3_InProcessServer_AoniClient(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH3Server(t, newEchoHandler())
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	// 1. GET request
	resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/h3-hello")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "HTTP/3.0", resp.Proto)

	// 2. POST request with body
	payload := "ping-h3-echo-data-payload"
	postReq, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL()+"/h3-echo",
		bytes.NewReader([]byte(payload)),
	)
	require.NoError(t, err)

	postResp, err := client.HTTP().Do(postReq)
	require.NoError(t, err)
	defer postResp.Body.Close()

	assert.Equal(t, http.StatusOK, postResp.StatusCode)
	assert.Equal(t, "HTTP/3.0", postResp.Proto)
	echoBytes, err := io.ReadAll(postResp.Body)
	require.NoError(t, err)
	assert.Equal(t, payload, string(echoBytes))
}

func TestM1_H3_AttachedConn_Mode(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH3Server(t, newEchoHandler())
	defer server.Close()

	cc, err := server.DialClientConn(context.Background())
	require.NoError(t, err)
	defer cc.Close()

	client := aoni.NewClient(nil, option.WithH3Conn(cc))
	defer client.Close()

	resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/attached-conn")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "HTTP/3.0", resp.Proto)
}

func TestM1_H3_ConcurrentStreamMultiplexing(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH3Server(t, newEchoHandler())
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		numWorkers    = 30
		reqsPerWorker = 10
	)

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*reqsPerWorker)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for r := 0; r < reqsPerWorker; r++ {
				path := fmt.Sprintf("/worker/%d/req/%d", workerID, r)
				payload := fmt.Sprintf("worker-%d-payload-%d", workerID, r)

				req, err := http.NewRequestWithContext(
					context.Background(),
					http.MethodPost,
					server.URL()+path,
					bytes.NewReader([]byte(payload)),
				)
				if err != nil {
					errCh <- fmt.Errorf("worker %d req %d create error: %w", workerID, r, err)
					return
				}
				req.Header.Set("X-Worker-ID", strconv.Itoa(workerID))
				req.Header.Set("X-Req-ID", strconv.Itoa(r))

				resp, err := client.HTTP().Do(req)
				if err != nil {
					errCh <- fmt.Errorf("worker %d req %d execute error: %w", workerID, r, err)
					return
				}

				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					errCh <- fmt.Errorf("worker %d req %d read error: %w", workerID, r, err)
					return
				}

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("worker %d req %d bad status: %d", workerID, r, resp.StatusCode)
					return
				}

				if string(body) != payload {
					errCh <- fmt.Errorf("worker %d req %d body mismatch: expected %q, got %q",
						workerID, r, payload, string(body))
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency error: %v", err)
	}
}

func TestM1_H3_ContextCancellation(t *testing.T) {
	defer testutil.Check(t)()

	// Slow server handler delaying response by 150ms
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	})

	server := testutil.NewH3Server(t, slowHandler)
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	// Short context deadline
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := client.Request(ctx, http.MethodGet, server.URL()+"/slow")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || aoni.IsTimeout(err))
}

func TestM1_H3_ResponseBodyMemoryHygiene(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH3Server(t, newEchoHandler())
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/hygiene")
	require.NoError(t, err)

	// Read body completely
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Multiple Close calls should be idempotent without panics or errors
	err1 := resp.Body.Close()
	assert.NoError(t, err1)
	err2 := resp.Body.Close()
	assert.NoError(t, err2)
}

func TestM1_H3_CloseIdleConnections(t *testing.T) {
	defer testutil.Check(t)()

	server := testutil.NewH3Server(t, newEchoHandler())
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/conn-1")
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Draining idle connections
	client.CloseIdleConnections()

	// Subsequent request should open a new connection seamlessly
	resp2, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/conn-2")
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}
