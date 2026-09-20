// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tests/testutil"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
)

// -----------------------------------------------------------------------------
// Test Server Instrumentation & Handlers
// -----------------------------------------------------------------------------

type SoakStats struct {
	TotalRequests  atomic.Int64
	ActiveRequests atomic.Int64
	MaxConcurrency atomic.Int64
	UniqueClients  sync.Map
}

func newSoakHandler(stats *SoakStats) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stats != nil {
			_, port, _ := net.SplitHostPort(r.RemoteAddr)
			if port != "" {
				stats.UniqueClients.Store(port, true)
			}

			active := stats.ActiveRequests.Add(1)
			defer stats.ActiveRequests.Add(-1)

			for {
				max := stats.MaxConcurrency.Load()
				if active <= max || stats.MaxConcurrency.CompareAndSwap(max, active) {
					break
				}
			}

			stats.TotalRequests.Add(1)
		}

		switch {
		case r.URL.Path == "/soak/ping":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Echo-Proto", r.Proto)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("pong"))

		case r.URL.Path == "/soak/echo":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("X-Echo-Length", strconv.Itoa(len(body)))
			w.Header().Set("X-Echo-Proto", r.Proto)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)

		case r.URL.Path == "/soak/jitter":
			time.Sleep(1 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("jitter-ok"))

		case r.URL.Path == "/soak/throttled":
			time.Sleep(15 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("throttled-ok"))

		case r.URL.Path == "/soak/slow":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(35 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("slow-done"))
			}

		case r.URL.Path == "/soak/stampede":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("stampede-ok"))

		case strings.HasPrefix(r.URL.Path, "/soak/h3/"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("X-Echo-Method", r.Method)
			w.Header().Set("X-Worker-ID", r.Header.Get("X-Worker-ID"))
			w.Header().Set("X-Req-ID", r.Header.Get("X-Req-ID"))
			w.Header().Set("X-Custom-Varying", r.Header.Get("X-Custom-Varying"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)

		case strings.HasPrefix(r.URL.Path, "/soak/sha256"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			hash := sha256.Sum256(body)
			w.Header().Set("X-SHA256", hex.EncodeToString(hash[:]))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("verified"))

		default:
			if strings.HasPrefix(r.URL.Path, "/item/") || strings.HasPrefix(r.URL.Path, "/burst/") {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("X-Echo-Proto", r.Proto)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
				return
			}
			http.NotFound(w, r)
		}
	})
}

// =============================================================================
// F5: HTTP/1.1 High-Concurrency Soak Tests
// =============================================================================

// TestSoak_H1_HighConcurrency_1000Reqs_50Workers verifies 1,000+ requests across
// 50 parallel workers with connection pool reuse, validating that socket count is bounded.
func TestSoak_H1_HighConcurrency_1000Reqs_50Workers(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     60 * time.Second,
		}),
	)
	defer client.Close()

	const (
		numWorkers    = 50
		reqsPerWorker = 20
		totalReqs     = numWorkers * reqsPerWorker
	)

	errCh := make(chan error, totalReqs)
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if r%2 == 0 {
					// GET Request
					resp, err := client.Get(ctx, server.URL()+"/soak/ping")
					if err != nil {
						cancel()
						errCh <- fmt.Errorf("worker %d GET %d failed: %w", workerID, r, err)
						return
					}
					body, err := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					cancel()
					if err != nil {
						errCh <- fmt.Errorf("worker %d GET %d read err: %w", workerID, r, err)
						return
					}
					if string(body) != "pong" {
						errCh <- fmt.Errorf("worker %d GET %d unexpected body: %s", workerID, r, body)
						return
					}
				} else {
					// POST Request with body
					payload := []byte(fmt.Sprintf("worker-%d-req-%d-data", workerID, r))
					resp, err := client.Post(ctx, server.URL()+"/soak/echo", bytes.NewReader(payload))
					if err != nil {
						cancel()
						errCh <- fmt.Errorf("worker %d POST %d failed: %w", workerID, r, err)
						return
					}
					body, err := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					cancel()
					if err != nil {
						errCh <- fmt.Errorf("worker %d POST %d read err: %w", workerID, r, err)
						return
					}
					if !bytes.Equal(body, payload) {
						errCh <- fmt.Errorf("worker %d POST %d payload mismatch", workerID, r)
						return
					}
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("H1 soak error: %v", err)
	}

	assert.Equal(t, int64(totalReqs), stats.TotalRequests.Load())

	// Connection reuse check: Count unique remote client ports seen by the server.
	// With 50 concurrent workers and keep-alives pooled, unique connections must be <= numWorkers + 25
	// to account for thread preemption timing jitter under the Go race detector while ensuring >= 92.5% reuse.
	uniqueConns := 0
	stats.UniqueClients.Range(func(key, value any) bool {
		uniqueConns++
		return true
	})
	assert.True(t, uniqueConns <= numWorkers+25,
		fmt.Sprintf("expected connection pooling reuse, got %d unique conns for %d requests", uniqueConns, totalReqs))
}

// TestSoak_H1_ThunderingHerd_BurstSoak tests synchronized burst dispatch of 1,500 requests
// across 100 workers via a release barrier.
func TestSoak_H1_ThunderingHerd_BurstSoak(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        150,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     60 * time.Second,
		}),
	)
	defer client.Close()

	const (
		numWorkers    = 100
		reqsPerWorker = 15
		totalReqs     = numWorkers * reqsPerWorker
	)

	// Pre-burst warmup: prime connection pool and OS socket routing with a single request
	// to avoid 100 simultaneous cold ConnectEx dials on Windows.
	var warmErr error
	backoff := 10 * time.Millisecond
	for attempt := 0; attempt < 3; attempt++ {
		warmCtx, warmCancel := context.WithTimeout(context.Background(), 5*time.Second)
		warmResp, getErr := client.Get(warmCtx, server.URL()+"/soak/ping")
		if getErr == nil {
			_, _ = io.Copy(io.Discard, warmResp.Body)
			_ = warmResp.Body.Close()
			warmCancel()
			warmErr = nil
			break
		}
		warmCancel()
		warmErr = getErr
		if !strings.Contains(getErr.Error(), "connectex") {
			break
		}
		if attempt < 2 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	require.NoError(t, warmErr)

	// Reset total requests counter so assertions accurately reflect the burst workload
	stats.TotalRequests.Store(0)

	barrier := make(chan struct{})
	var readyWg sync.WaitGroup
	var doneWg sync.WaitGroup
	errCh := make(chan error, totalReqs)

	for w := 0; w < numWorkers; w++ {
		readyWg.Add(1)
		doneWg.Add(1)
		go func(workerID int) {
			readyWg.Done()
			<-barrier // Wait for simultaneous release
			defer doneWg.Done()

			for r := 0; r < reqsPerWorker; r++ {
				var err error
				for attempt := 0; attempt < 3; attempt++ {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					resp, getErr := client.Get(ctx, server.URL()+"/soak/ping")
					if getErr == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
						cancel()
						err = nil
						break
					}
					cancel()
					err = getErr
					if !strings.Contains(getErr.Error(), "connectex") {
						break
					}
					time.Sleep(2 * time.Millisecond)
				}
				if err != nil {
					errCh <- fmt.Errorf("worker %d req %d err: %w", workerID, r, err)
					return
				}
			}
		}(w)
	}

	readyWg.Wait()
	close(barrier) // Release all workers simultaneously
	doneWg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Thundering herd error: %v", err)
	}

	assert.Equal(t, int64(totalReqs), stats.TotalRequests.Load())
}

// TestSoak_H1_Baremetal_vs_Pipeline_Soak verifies parallel execution across both
// baremetal zero-alloc bypass and the 5-stage middleware pipeline without data races.
func TestSoak_H1_Baremetal_vs_Pipeline_Soak(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	// Baremetal client: no modifiers, no request config
	baremetalClient := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     60 * time.Second,
		}),
	)
	defer baremetalClient.Close()

	// Pipeline client: defaults and modifiers active
	pipelineClient := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     60 * time.Second,
		}),
		option.WithHeader("X-Pipeline-Active", "true"),
	)
	defer pipelineClient.Close()

	const (
		workersPerPath = 25
		reqsPerWorker  = 20
		totalExpected  = workersPerPath * reqsPerWorker * 2
	)

	var wg sync.WaitGroup
	errCh := make(chan error, totalExpected)

	// Launch Baremetal workers
	for w := 0; w < workersPerPath; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				resp, err := baremetalClient.Get(ctx, server.URL()+"/soak/ping")
				if err != nil {
					cancel()
					errCh <- fmt.Errorf("baremetal w%d r%d err: %w", id, r, err)
					return
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()
				if err != nil || string(body) != "pong" {
					errCh <- fmt.Errorf("baremetal w%d r%d invalid response", id, r)
					return
				}
			}
		}(w)
	}

	// Launch Pipeline workers
	for w := 0; w < workersPerPath; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				payload := []byte(fmt.Sprintf("pipeline-w%d-r%d", id, r))
				resp, err := pipelineClient.Request(ctx, http.MethodPost, server.URL()+"/soak/echo",
					mod.WithHeader("X-Worker-Tag", strconv.Itoa(id)),
					mod.WithBody(bytes.NewReader(payload)),
				)
				if err != nil {
					cancel()
					errCh <- fmt.Errorf("pipeline w%d r%d err: %w", id, r, err)
					return
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()
				if err != nil || !bytes.Equal(body, payload) {
					errCh <- fmt.Errorf("pipeline w%d r%d payload mismatch", id, r)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Baremetal vs Pipeline error: %v", err)
	}

	assert.Equal(t, int64(totalExpected), stats.TotalRequests.Load())
}

// TestSoak_FastClient_H1_Soak exercises fast.Client across 1,000 requests over
// native transport.Pool and mach/client/h1 engine.
func TestSoak_FastClient_H1_Soak(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	fc := fast.NewClient()
	defer fc.Engine().CloseIdleConnections()

	const (
		numWorkers    = 50
		reqsPerWorker = 20
		totalReqs     = numWorkers * reqsPerWorker
	)

	var wg sync.WaitGroup
	errCh := make(chan error, totalReqs)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				req := fast.NewRequest(nil)
				req.SetMethod(http.MethodGet)
				req.SetURL(server.URL() + "/soak/ping")

				resp, err := fc.Do(req)
				req.Release()
				if err != nil {
					errCh <- fmt.Errorf("fast client w%d r%d err: %w", id, r, err)
					return
				}

				if resp.StatusCode() != http.StatusOK {
					errCh <- fmt.Errorf("fast client w%d r%d status: %d", id, r, resp.StatusCode())
					_ = resp.Close()
					return
				}
				_ = resp.Close()
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("FastClient soak error: %v", err)
	}

	assert.Equal(t, int64(totalReqs), stats.TotalRequests.Load())
}

// =============================================================================
// F6: HTTP/2 High-Concurrency Multiplex Soak Tests
// =============================================================================

// TestSoak_H2_Multiplex_1000Parallel_Burst exercises 1,000 multiplexed HTTP/2 requests
// dispatched concurrently across streams and connections.
func TestSoak_H2_Multiplex_1000Parallel_Burst(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(8*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH2Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(server.Client())
	defer client.Close()

	const (
		numWorkers    = 50
		reqsPerWorker = 20
		totalRequests = numWorkers * reqsPerWorker
	)
	var readyWg sync.WaitGroup
	var doneWg sync.WaitGroup
	startBarrier := make(chan struct{})
	errCh := make(chan error, totalRequests)
	var completedCount atomic.Int64

	for w := 0; w < numWorkers; w++ {
		readyWg.Add(1)
		doneWg.Add(1)
		go func(workerID int) {
			readyWg.Done()
			<-startBarrier // Synchronized burst release
			defer doneWg.Done()

			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				reqID := workerID*reqsPerWorker + r

				payload := []byte(fmt.Sprintf("h2-payload-%d", reqID))
				req, err := http.NewRequestWithContext(
					ctx,
					http.MethodPost,
					server.URL()+"/item/"+strconv.Itoa(reqID),
					bytes.NewReader(payload),
				)
				if err != nil {
					cancel()
					errCh <- err
					return
				}

				resp, err := client.HTTP().Do(req)
				if err != nil {
					cancel()
					errCh <- fmt.Errorf("h2 burst w%d r%d err: %w", workerID, r, err)
					return
				}

				if resp.StatusCode != http.StatusOK {
					_ = resp.Body.Close()
					cancel()
					errCh <- fmt.Errorf("h2 burst w%d r%d bad status: %d", workerID, r, resp.StatusCode)
					return
				}
				if resp.Proto != "HTTP/2.0" {
					_ = resp.Body.Close()
					cancel()
					errCh <- fmt.Errorf("h2 burst w%d r%d unexpected proto: %s", workerID, r, resp.Proto)
					return
				}

				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()
				if err != nil {
					errCh <- fmt.Errorf("h2 burst w%d r%d read err: %w", workerID, r, err)
					return
				}
				if !bytes.Equal(body, payload) {
					errCh <- fmt.Errorf("h2 burst w%d r%d payload mismatch", workerID, r)
					return
				}

				completedCount.Add(1)
			}
		}(w)
	}

	readyWg.Wait()
	close(startBarrier)
	doneWg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("H2 multiplex burst error: %v", err)
	}

	assert.Equal(t, int64(totalRequests), completedCount.Load())
	assert.Equal(t, int64(totalRequests), stats.TotalRequests.Load())
}

// TestSoak_H2_FlowControl_LargePayloads streams 100 concurrent 128 KB payloads over HTTP/2,
// exceeding default 65,535-byte window and verifying WINDOW_UPDATE handling without deadlocks.
func TestSoak_H2_FlowControl_LargePayloads(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(10*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH2Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(server.Client())
	defer client.Close()

	const (
		numStreams  = 100
		payloadSize = 128 * 1024 // 128 KB
	)

	var wg sync.WaitGroup
	errCh := make(chan error, numStreams)

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(streamID int) {
			defer wg.Done()

			pattern := byte(streamID % 256)
			payload := bytes.Repeat([]byte{pattern}, payloadSize)
			expectedHash := sha256.Sum256(payload)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(
				ctx,
				http.MethodPost,
				server.URL()+"/soak/echo",
				bytes.NewReader(payload),
			)
			if err != nil {
				errCh <- err
				return
			}

			resp, err := client.HTTP().Do(req)
			if err != nil {
				errCh <- fmt.Errorf("stream %d err: %w", streamID, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("stream %d bad status: %d", streamID, resp.StatusCode)
				return
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errCh <- fmt.Errorf("stream %d read err: %w", streamID, err)
				return
			}

			actualHash := sha256.Sum256(body)
			if actualHash != expectedHash {
				errCh <- fmt.Errorf("stream %d hash mismatch", streamID)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("H2 flow control error: %v", err)
	}
}

// TestSoak_H2_MidFlight_CancellationStorm verifies RST_STREAM handling and resource cleanup
// when 250 requests are cancelled mid-flight while 250 healthy requests proceed concurrently.
func TestSoak_H2_MidFlight_CancellationStorm(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(8*time.Second))()

	server := testutil.NewH2Server(t, newSoakHandler(nil))
	defer server.Close()

	client := aoni.NewClient(server.Client())
	defer client.Close()

	// Warmup request to establish the HTTP/2 TLS connection prior to stream cancellation storm
	warmupCtx, warmupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	respWarmup, err := client.Get(warmupCtx, server.URL()+"/soak/ping")
	warmupCancel()
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, respWarmup.Body)
	_ = respWarmup.Body.Close()

	const (
		workersPerGroup = 25
		reqsPerWorker   = 10
		expectedHealthy = workersPerGroup * reqsPerWorker
		expectedCancel  = workersPerGroup * reqsPerWorker
	)

	var wg sync.WaitGroup
	var healthyPassed atomic.Int64
	var cancelHandled atomic.Int64

	// 1. Healthy workers executing GET requests on /soak/ping
	for w := 0; w < workersPerGroup; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/soak/ping", nil)
				resp, err := client.HTTP().Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						healthyPassed.Add(1)
					}
				}
				cancel()
			}
		}()
	}

	// 2. Cancelling workers hitting /soak/slow with 3ms timeout (triggers RST_STREAM mid-flight)
	for w := 0; w < workersPerGroup; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Millisecond)
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/soak/slow", nil)
				resp, err := client.HTTP().Do(req)
				if err != nil {
					cancelHandled.Add(1)
				} else {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
				cancel()
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int64(expectedHealthy), healthyPassed.Load())
	assert.GreaterOrEqual(t, cancelHandled.Load(), int64(expectedCancel)/2)
}

// TestSoak_H2_MultiConnection_PoolScaling tests 4 successive waves of 250 concurrent requests
// verifying connection pooling and multiplexing scale-out across waves.
func TestSoak_H2_MultiConnection_PoolScaling(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(8*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH2Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(server.Client())
	defer client.Close()

	const (
		waves         = 4
		reqsPerWave   = 250
		totalExpected = waves * reqsPerWave
	)

	for wave := 0; wave < waves; wave++ {
		const (
			workersPerWave = 50
			reqsPerWorker  = 5
		)
		var wg sync.WaitGroup
		errCh := make(chan error, reqsPerWave)

		for w := 0; w < workersPerWave; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for r := 0; r < reqsPerWorker; r++ {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/soak/ping", nil)
					if err != nil {
						cancel()
						errCh <- err
						return
					}

					resp, err := client.HTTP().Do(req)
					if err != nil {
						cancel()
						errCh <- err
						return
					}
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					cancel()
				}
			}(w)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("Wave %d error: %v", wave, err)
		}
	}

	assert.Equal(t, int64(totalExpected), stats.TotalRequests.Load())
}

// =============================================================================
// F7: HTTP/3 High-Concurrency Soak Tests over QUIC & QPACK
// =============================================================================

// TestSoak_H3_HighConcurrency_1000Requests_Multiplexed verifies 1,000 requests over
// a single multiplexed QUIC connection with diverse HTTP methods, varied payloads,
// and dynamic headers exercising QPACK table operations.
func TestSoak_H3_HighConcurrency_1000Requests_Multiplexed(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(10*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH3Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		numWorkers    = 100
		reqsPerWorker = 10
		totalExpected = numWorkers * reqsPerWorker
	)

	var wg sync.WaitGroup
	errCh := make(chan error, totalExpected)
	var completedCount atomic.Int64

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			method := methods[workerID%len(methods)]

			for r := 0; r < reqsPerWorker; r++ {
				var bodyReader io.Reader
				var expectedBody []byte

				if method != http.MethodGet {
					expectedBody = []byte(fmt.Sprintf("w%d-r%d-%s", workerID, r, bytes.Repeat([]byte("K"), (workerID%4)*512)))
					bodyReader = bytes.NewReader(expectedBody)
				}

				req, err := http.NewRequestWithContext(
					context.Background(),
					method,
					fmt.Sprintf("%s/soak/h3/w%d/r%d", server.URL(), workerID, r),
					bodyReader,
				)
				if err != nil {
					errCh <- fmt.Errorf("w%d r%d req creation err: %w", workerID, r, err)
					return
				}

				req.Header.Set("X-Worker-ID", strconv.Itoa(workerID))
				req.Header.Set("X-Req-ID", strconv.Itoa(r))
				req.Header.Set("X-Custom-Varying", fmt.Sprintf("val-%d-%d", workerID, r))

				resp, err := client.HTTP().Do(req)
				if err != nil {
					errCh <- fmt.Errorf("w%d r%d exec err: %w", workerID, r, err)
					return
				}

				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					errCh <- fmt.Errorf("w%d r%d read err: %w", workerID, r, err)
					return
				}

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("w%d r%d bad status: %d", workerID, r, resp.StatusCode)
					return
				}

				if !bytes.Equal(body, expectedBody) {
					errCh <- fmt.Errorf("w%d r%d payload mismatch", workerID, r)
					return
				}

				completedCount.Add(1)
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("H3 multiplex soak failure: %v", err)
	}

	assert.Equal(t, int64(totalExpected), completedCount.Load())
}

// TestSoak_H3_StreamQueue_Backpressure_1000Requests launches 200 workers simultaneously
// against an HTTP/3 server with default stream concurrency limit (100), executing 5 requests
// each (1,000 total requests) via a synchronized barrier, verifying client FIFO queueing,
// stream limit backpressure, and orderly unblocking without deadlocks.
func TestSoak_H3_StreamQueue_Backpressure_1000Requests(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(12*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH3Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		numWorkers    = 200
		reqsPerWorker = 5
		totalRequests = numWorkers * reqsPerWorker
	)
	startBarrier := make(chan struct{})
	var readyWg sync.WaitGroup
	var doneWg sync.WaitGroup
	errCh := make(chan error, totalRequests)
	var completed atomic.Int64

	for i := 0; i < numWorkers; i++ {
		readyWg.Add(1)
		doneWg.Add(1)
		go func(workerID int) {
			readyWg.Done()
			<-startBarrier
			defer doneWg.Done()

			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/soak/stampede", nil)
				if err != nil {
					cancel()
					errCh <- err
					return
				}

				resp, err := client.HTTP().Do(req)
				if err != nil {
					cancel()
					errCh <- fmt.Errorf("w%d r%d exec err: %w", workerID, r, err)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				cancel()

				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("w%d r%d status: %d", workerID, r, resp.StatusCode)
					return
				}
				completed.Add(1)
			}
		}(i)
	}

	readyWg.Wait()
	close(startBarrier)
	doneWg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Stream queue backpressure error: %v", err)
	}

	assert.Equal(t, int64(totalRequests), completed.Load())
}

// TestSoak_H3_FlowControl_LargePayloadSaturation saturates QUIC flow control windows
// with 20 parallel streams transferring 512 KB to 896 KB each (>14 MB total) and validates SHA-256 integrity.
func TestSoak_H3_FlowControl_LargePayloadSaturation(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(15*time.Second))()

	server := testutil.NewH3Server(t, newSoakHandler(nil))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const numWorkers = 20
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			payloadSize := 512*1024 + (id%4)*128*1024 // 512KB to 896KB
			payload := bytes.Repeat([]byte(fmt.Sprintf("%02x", id)), payloadSize/2)
			expectedHash := sha256.Sum256(payload)
			expectedHashStr := hex.EncodeToString(expectedHash[:])

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(
				ctx,
				http.MethodPost,
				server.URL()+"/soak/sha256",
				bytes.NewReader(payload),
			)
			if err != nil {
				errCh <- err
				return
			}

			resp, err := client.HTTP().Do(req)
			if err != nil {
				errCh <- fmt.Errorf("large payload %d err: %w", id, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("large payload %d status: %d", id, resp.StatusCode)
				return
			}

			actualHash := resp.Header.Get("X-SHA256")
			if actualHash != expectedHashStr {
				errCh <- fmt.Errorf("large payload %d hash mismatch: expected %s, got %s", id, expectedHashStr, actualHash)
				return
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("H3 flow control saturation error: %v", err)
	}
}

// TestSoak_H3_Turbulence_RapidCancellations tests stream cancellation and watcher cleanup
// under heavy concurrent load (100 healthy requests, 50 timeouts, 50 explicit cancellations).
func TestSoak_H3_Turbulence_RapidCancellations(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(10*time.Second))()

	server := testutil.NewH3Server(t, newSoakHandler(nil))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		numHealthy = 100
		numTimeout = 50
		numCancel  = 50
	)
	var wg sync.WaitGroup
	var healthySuccess atomic.Int64
	var cancelledHandled atomic.Int64

	// 1. Healthy workers
	for i := 0; i < numHealthy; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/soak/ping", nil)
			resp, err := client.HTTP().Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					healthySuccess.Add(1)
				}
			}
		}(i)
	}

	// 2. Timeout workers
	for i := 0; i < numTimeout; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()

			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/soak/slow", nil)
			resp, err := client.HTTP().Do(req)
			if err != nil {
				cancelledHandled.Add(1)
			} else {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}(i)
	}

	// 3. Explicit Cancel workers
	for i := 0; i < numCancel; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(2 * time.Millisecond)
				cancel()
			}()

			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/soak/slow", nil)
			resp, err := client.HTTP().Do(req)
			if err != nil {
				cancelledHandled.Add(1)
			} else {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(numHealthy), healthySuccess.Load())
	assert.GreaterOrEqual(t, cancelledHandled.Load(), int64(numTimeout+numCancel)/2)
}

// TestSoak_H3_ConnectionCycling_PoolRecycling verifies that CloseIdleConnections()
// drains active QUIC connections and subsequent bursts transparently re-dial without leaks.
func TestSoak_H3_ConnectionCycling_PoolRecycling(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(10*time.Second))()

	server := testutil.NewH3Server(t, newSoakHandler(nil))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		bursts        = 4
		numConcurrent = 49
		reqsPerBurst  = 1 + numConcurrent
	)

	for b := 0; b < bursts; b++ {
		// 1. Initial request in burst establishes the fresh connection after CloseIdleConnections
		initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
		respInit, err := client.Request(initCtx, http.MethodGet, fmt.Sprintf("%s/burst/%d/init", server.URL(), b))
		initCancel()
		require.NoError(t, err, fmt.Sprintf("burst %d initial connection dial failed", b))
		_, _ = io.Copy(io.Discard, respInit.Body)
		_ = respInit.Body.Close()
		require.Equal(t, http.StatusOK, respInit.StatusCode)

		// 2. Concurrent wave multiplexed over the newly established connection
		var wg sync.WaitGroup
		errCh := make(chan error, numConcurrent)
		var success atomic.Int64
		success.Add(1) // from respInit

		for i := 0; i < numConcurrent; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				resp, err := client.Request(ctx, http.MethodGet, fmt.Sprintf("%s/burst/%d/%d", server.URL(), b, id))
				if err != nil {
					errCh <- fmt.Errorf("burst %d req %d err: %w", b, id, err)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					success.Add(1)
				}
			}(i)
		}
		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("Burst %d failure: %v", b, err)
		}

		require.Equal(t, int64(reqsPerBurst), success.Load(), fmt.Sprintf("burst %d failed", b))

		// Drain pooled connection between bursts and allow QUIC draining period (3*PTO) to complete
		client.CloseIdleConnections()
		time.Sleep(300 * time.Millisecond)
	}
}

// =============================================================================
// F8: Connection Pool Dynamic Stress & Idle Recycling Tests
// =============================================================================

// TestSoak_Pool_ConcurrentRapidCloseIdle exercises 1,000 requests while a background
// goroutine continuously invokes CloseIdleConnections() every 10ms.
func TestSoak_Pool_ConcurrentRapidCloseIdle(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     60 * time.Second,
		}),
	)
	defer client.Close()

	const (
		numWorkers    = 50
		reqsPerWorker = 20
		totalReqs     = numWorkers * reqsPerWorker
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Background rapid idle flusher
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				client.CloseIdleConnections()
			}
		}
	}()

	errCh := make(chan error, totalReqs)
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				reqCtx, reqCancel := context.WithTimeout(context.Background(), 5*time.Second)
				resp, err := client.Get(reqCtx, server.URL()+"/soak/jitter")
				if err != nil {
					reqCancel()
					errCh <- err
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				reqCancel()
			}
		}(w)
	}

	wg.Wait()
	cancel() // Stop flusher
	close(errCh)

	for err := range errCh {
		t.Fatalf("Rapid close idle stress error: %v", err)
	}

	assert.Equal(t, int64(totalReqs), stats.TotalRequests.Load())
}

// TestSoak_Pool_DynamicConfigMutation verifies safe concurrent mutation and cloning
// of client pool configurations under load.
func TestSoak_Pool_DynamicConfigMutation(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	baseClient := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
		}),
	)
	defer baseClient.Close()

	const (
		numWorkers    = 50
		reqsPerWorker = 10
		totalReqs     = numWorkers * reqsPerWorker
	)

	poolLimits := []int{5, 20, 50, 100}
	var wg sync.WaitGroup
	errCh := make(chan error, totalReqs)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Dynamically mutate pool config per worker
			targetLimit := poolLimits[id%len(poolLimits)]
			cloned := baseClient.With(option.WithConnectionPool(aoni.ConnectionPoolConfig{
				MaxIdleConns:        targetLimit * 2,
				MaxIdleConnsPerHost: targetLimit,
			}))
			defer cloned.Close()

			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				resp, err := cloned.Get(ctx, server.URL()+"/soak/ping")
				if err != nil {
					cancel()
					errCh <- err
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				cancel()
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Dynamic config mutation error: %v", err)
	}

	assert.Equal(t, int64(totalReqs), stats.TotalRequests.Load())
}

// TestSoak_Pool_MaxConnsPerHost_Throttling enforces that MaxConnsPerHost strictly
// bounds the peak concurrent requests observed by the server.
func TestSoak_Pool_MaxConnsPerHost_Throttling(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	const maxConns = 8

	client := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxConnsPerHost:     maxConns,
			MaxIdleConnsPerHost: maxConns,
		}),
	)
	defer client.Close()

	const numWorkers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := client.Get(ctx, server.URL()+"/soak/throttled")
			if err != nil {
				errCh <- err
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Throttling error: %v", err)
	}

	// Verify server observed peak concurrency never exceeded MaxConnsPerHost
	observedMax := stats.MaxConcurrency.Load()
	assert.True(t, observedMax <= maxConns,
		fmt.Sprintf("expected peak concurrency <= %d, observed %d", maxConns, observedMax))
}

// TestSoak_Pool_AggressiveIdleTimeout_Recycling tests 4 waves of 100 requests separated
// by 40ms pauses against an IdleConnTimeout of 20ms, verifying idle connection sweeping and re-dialing.
func TestSoak_Pool_AggressiveIdleTimeout_Recycling(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	stats := &SoakStats{}
	server := testutil.NewH1Server(t, newSoakHandler(stats))
	defer server.Close()

	// 20ms idle timeout forces rapid connection teardown during pauses
	client := aoni.NewClient(nil,
		option.WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     20 * time.Millisecond,
		}),
	)
	defer client.Close()

	const (
		waves         = 4
		numWorkers    = 20
		reqsPerWorker = 5
		reqsPerWave   = numWorkers * reqsPerWorker // 100
		totalExpected = waves * reqsPerWave        // 400
	)

	for wave := 0; wave < waves; wave++ {
		var wg sync.WaitGroup
		errCh := make(chan error, reqsPerWave)

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for r := 0; r < reqsPerWorker; r++ {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					resp, err := client.Get(ctx, server.URL()+"/soak/ping")
					if err != nil {
						cancel()
						errCh <- fmt.Errorf("wave %d worker %d req %d err: %w", wave, workerID, r, err)
						return
					}
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					cancel()
				}
			}(w)
		}

		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Fatalf("Wave %d error: %v", wave, err)
		}

		// Sleep 40ms to guarantee idle connections exceed 20ms timeout and are swept
		time.Sleep(40 * time.Millisecond)
	}

	assert.Equal(t, int64(totalExpected), stats.TotalRequests.Load())

	// Connection recycling verification:
	// Across 4 waves, 20 connections per wave are dialed and swept after 40ms pause.
	// Total unique connections must be bounded (~80), verifying connection reuse within waves
	// and re-dialing after idle timeout sweep across waves.
	uniqueConns := 0
	stats.UniqueClients.Range(func(key, value any) bool {
		uniqueConns++
		return true
	})
	assert.True(t, uniqueConns <= waves*numWorkers+10,
		fmt.Sprintf("expected <= %d unique conns for pooled idle recycling, got %d", waves*numWorkers+10, uniqueConns))
	assert.True(t, uniqueConns >= (waves-1)*numWorkers,
		fmt.Sprintf("expected >= %d unique conns due to idle sweeps, got %d", (waves-1)*numWorkers, uniqueConns))
}

// Silence unused variable warnings if any
var _ = errors.New
