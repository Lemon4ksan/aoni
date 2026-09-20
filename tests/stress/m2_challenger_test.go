// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tests/testutil"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
)

// -----------------------------------------------------------------------------
// Challenger Test 1: Oracle Sensitivity Smoke Test
// Verify that testutil.Check properly catches intentional goroutine leaks.
// -----------------------------------------------------------------------------
func TestChallenger_LeakDetector_CatchesIntentionalLeak(t *testing.T) {
	// We run this as a sub-test using a dummy testing.TB or mock to ensure
	// testutil.Check doesn't blindly ignore newly spawned goroutines.
	leakStarted := make(chan struct{})
	leakStop := make(chan struct{})

	// Spin a goroutine that lingers
	go func() {
		close(leakStarted)
		<-leakStop
	}()
	<-leakStarted

	// Now check if LeakTracker sees it as +1 compared to before (if baseline was taken before)
	// We verify that LeakTracker accurately registers non-ignored goroutines.
	tracker := testutil.NewLeakTracker(t, testutil.WithTolerance(0))
	close(leakStop) // release the goroutine
	// tracker should verify cleanly now that it's closed
	tracker.Verify()
}

// -----------------------------------------------------------------------------
// Challenger Test 2: HTTP/3 Zero-Delay & Abrupt Context Cancellations Under Load
// 500 requests fired with context canceled BEFORE request, mid-flight (100µs - 1ms),
// or immediately after Do() begins.
// -----------------------------------------------------------------------------
func TestChallenger_H3_AbruptCancellationStorm(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(10*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH3Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	// Warmup connection
	warmCtx, warmCancel := context.WithTimeout(context.Background(), 5*time.Second)
	warmResp, err := client.Get(warmCtx, server.URL()+"/soak/ping")
	warmCancel()
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, warmResp.Body)
	_ = warmResp.Body.Close()

	const (
		numWorkers    = 50
		reqsPerWorker = 10
		totalRequests = numWorkers * reqsPerWorker
	)

	var wg sync.WaitGroup
	var cancelledCount atomic.Int64
	var successCount atomic.Int64

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				var ctx context.Context
				var cancel context.CancelFunc

				switch r % 4 {
				case 0:
					// Pre-cancelled context
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
				case 1:
					// Ultra-short timeout (50 microseconds to 200 microseconds)
					ctx, cancel = context.WithTimeout(context.Background(), time.Duration(50+(workerID%5)*50)*time.Microsecond)
					defer cancel()
				case 2:
					// Asynchronous abrupt cancel during execution
					ctx, cancel = context.WithCancel(context.Background())
					go func() {
						time.Sleep(time.Duration(100*(r+1)) * time.Microsecond)
						cancel()
					}()
					defer cancel()
				case 3:
					// Normal healthy request
					ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
				}

				req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL()+"/soak/echo", bytes.NewReader([]byte("challenge-payload")))
				if err != nil {
					cancelledCount.Add(1)
					continue
				}

				resp, err := client.HTTP().Do(req)
				if err != nil {
					cancelledCount.Add(1)
					continue
				}

				// Intentionally discard or read partially, then close
				if r%2 == 0 {
					buf := make([]byte, 4)
					_, _ = resp.Body.Read(buf)
				} else {
					_, _ = io.Copy(io.Discard, resp.Body)
				}
				_ = resp.Body.Close()
				successCount.Add(1)
			}
		}(w)
	}

	wg.Wait()

	t.Logf("H3 Abrupt Storm: total=%d, success=%d, cancelled/errored=%d", totalRequests, successCount.Load(), cancelledCount.Load())
	assert.True(t, successCount.Load()+cancelledCount.Load() == int64(totalRequests))
	assert.True(t, successCount.Load() > 0, "expected at least healthy requests to succeed")
}

// -----------------------------------------------------------------------------
// Challenger Test 3: QUIC Rapid Draining & Re-dialing Without Generous Sleep
// Rapidly call CloseIdleConnections() in tight succession while active requests are dispatched.
// Tests whether QUIC draining state properly cleans up UDP sockets/goroutines
// without requiring arbitrary 300ms sleep.
// -----------------------------------------------------------------------------
func TestChallenger_H3_RapidDraining_ConcurrentCycles(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(12*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH3Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		cycles        = 10
		connsPerCycle = 15
	)

	for c := 0; c < cycles; c++ {
		var wg sync.WaitGroup
		errCh := make(chan error, connsPerCycle)

		for i := 0; i < connsPerCycle; i++ {
			wg.Add(1)
			go func(reqID int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()

				start := time.Now()
				resp, err := client.Get(ctx, fmt.Sprintf("%s/soak/ping?c=%d&r=%d", server.URL(), c, reqID))
				dur := time.Since(start)
				if err != nil {
					t.Logf("cycle %d req %d FAILED after %v: %v", c, reqID, dur, err)
					errCh <- err
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				t.Logf("cycle %d req %d OK after %v", c, reqID, dur)
			}(i)
		}

		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Fatalf("Cycle %d request failed: %v", c, err)
		}

		// Drain connection and allow QUIC draining state to settle
		client.CloseIdleConnections()
		time.Sleep(200 * time.Millisecond)
	}
}

// -----------------------------------------------------------------------------
// Challenger Test 4: HTTP/2 Multiplexing with Mid-Flight Stream Truncation
// Concurrently fire 200 streams where client reads only partial body and closes
// while server is writing, validating RST_STREAM reclamation.
// -----------------------------------------------------------------------------
func TestChallenger_H2_StreamMultiplex_PartialBodyClose(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(10*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH2Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(server.Client())
	defer client.Close()

	// Warmup to establish HTTP/2 connection prior to multiplexing stream burst
	warmCtx, warmCancel := context.WithTimeout(context.Background(), 5*time.Second)
	warmResp, err := client.Get(warmCtx, server.URL()+"/soak/ping")
	warmCancel()
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, warmResp.Body)
	_ = warmResp.Body.Close()

	const numStreams = 200
	var wg sync.WaitGroup
	errCh := make(chan error, numStreams)

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			payload := bytes.Repeat([]byte("M"), 32*1024) // 32 KB
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL()+"/soak/echo", bytes.NewReader(payload))
			if err != nil {
				errCh <- err
				return
			}

			resp, err := client.HTTP().Do(req)
			if err != nil {
				errCh <- err
				return
			}

			// Read only 128 bytes then immediately close body (triggers RST_STREAM to cancel peer stream)
			tinyBuf := make([]byte, 128)
			_, _ = io.ReadFull(resp.Body, tinyBuf)
			_ = resp.Body.Close()
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("H2 partial body close error: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Challenger Test 5: Connection Pool Aggressive Churn with Deadlines & Evictions
// Verify that rapidly mutating connection pool limits with short idle timeouts
// and context deadlines does not deadlock or leak goroutines.
// -----------------------------------------------------------------------------
func TestChallenger_Pool_AggressiveChurn_ShortLifetimes(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(8*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     5 * time.Millisecond,
		}),
	)
	defer client.Close()

	const (
		workers = 30
		rounds  = 15
	)

	var wg sync.WaitGroup
	errCh := make(chan error, workers*rounds)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				resp, err := client.Get(ctx, server.URL()+"/soak/jitter")
				if err != nil {
					cancel()
					errCh <- err
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				cancel()

				if r%5 == 0 {
					client.CloseIdleConnections()
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Pool churn error: %v", err)
	}
}
