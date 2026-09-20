// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	machhttp "github.com/lemon4ksan/mach/proto/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tests/testutil"
)

// -----------------------------------------------------------------------------
// Challenger Empirical Bug Reproduction: Multi-Header In-Place Corruption
// -----------------------------------------------------------------------------

func TestM1_Challenger_H3_MultiHeaderCorruption(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	var (
		mu              sync.Mutex
		receivedHeaders http.Header
	)
	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL()+"/headers", nil)
	require.NoError(t, err)

	req.Header.Set("X-Header-Alpha", "val-alpha")
	req.Header.Set("X-Header-Beta", "val-beta")
	req.Header.Set("X-Header-Gamma", "val-gamma")

	resp, err := client.HTTP().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	mu.Lock()
	hdrs := receivedHeaders.Clone()
	mu.Unlock()

	t.Logf("Headers received by server: %#v", hdrs)

	// Assert that all three headers were received with their exact expected names and values
	assert.Equal(t, "val-alpha", hdrs.Get("X-Header-Alpha"))
	assert.Equal(t, "val-beta", hdrs.Get("X-Header-Beta"))
	assert.Equal(t, "val-gamma", hdrs.Get("X-Header-Gamma"))
}

// -----------------------------------------------------------------------------
// Challenger Test 1: Direct ClientConn Concurrency Multiplexing (100 Workers)
// -----------------------------------------------------------------------------

func TestM1_Challenger_ClientConn_HighConcurrency_100Workers(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	ctx := context.Background()
	cc, err := server.DialClientConn(ctx)
	require.NoError(t, err)
	defer cc.Close()

	const (
		numWorkers    = 100
		reqsPerWorker = 10
	)

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*reqsPerWorker)
	var completedReqs atomic.Int64

	// Pre-generate varied payloads
	payloadEmpty := []byte("")
	payloadSmall := bytes.Repeat([]byte("A"), 64)
	payloadMedium := bytes.Repeat([]byte("B"), 2048)
	payloadLarge := bytes.Repeat([]byte("C"), 16384)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Select payload based on worker class
			var payload []byte
			switch workerID % 4 {
			case 0:
				payload = payloadEmpty
			case 1:
				payload = payloadSmall
			case 2:
				payload = payloadMedium
			case 3:
				payload = payloadLarge
			}

			for r := 0; r < reqsPerWorker; r++ {
				req := machhttp.AcquireRequest()
				resp := machhttp.AcquireResponse()

				reqPath := fmt.Sprintf("/worker/%d/req/%d", workerID, r)
				req.Header.SetMethod(http.MethodPost)
				req.SetRequestURI(server.URL() + reqPath)
				req.Header.SetHost(server.Addr())
				req.Header.SetContentLength(len(payload))
				req.SetBody(payload)

				_, err := cc.Do(ctx, req, resp, nil)
				if err != nil {
					machhttp.ReleaseRequest(req)
					machhttp.ReleaseResponse(resp)
					errCh <- fmt.Errorf("worker %d req %d cc.Do failed: %w", workerID, r, err)
					return
				}

				if resp.StatusCode() != http.StatusOK {
					status := resp.StatusCode()
					machhttp.ReleaseRequest(req)
					machhttp.ReleaseResponse(resp)
					errCh <- fmt.Errorf("worker %d req %d bad status: %d", workerID, r, status)
					return
				}

				body := resp.Body()
				if !bytes.Equal(body, payload) {
					expectedLen := len(payload)
					actualLen := len(body)
					status := resp.StatusCode()
					machhttp.ReleaseRequest(req)
					machhttp.ReleaseResponse(resp)
					errCh <- fmt.Errorf("worker %d req %d payload mismatch: expected %d bytes, got %d bytes, status=%d",
						workerID, r, expectedLen, actualLen, status)
					return
				}

				machhttp.ReleaseRequest(req)
				machhttp.ReleaseResponse(resp)
				completedReqs.Add(1)
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("multiplexing error: %v", err)
	}

	assert.Equal(t, int64(numWorkers*reqsPerWorker), completedReqs.Load())
}

// -----------------------------------------------------------------------------
// Challenger Test 2: aoni.Client + H3Engine Concurrency Multiplexing (100 Workers)
// -----------------------------------------------------------------------------

func TestM1_Challenger_H3Engine_HighConcurrency_100Workers(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Method", r.Method)
		w.Header().Set("X-Worker-ID", r.Header.Get("X-Worker-ID"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		numWorkers    = 100
		reqsPerWorker = 10
	)

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*reqsPerWorker)
	var completedReqs atomic.Int64

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
					expectedBody = []byte(fmt.Sprintf("worker-%d-req-%d-data-chunk", workerID, r))
					bodyReader = bytes.NewReader(expectedBody)
				}

				req, err := http.NewRequestWithContext(
					context.Background(),
					method,
					fmt.Sprintf("%s/engine/w%d/r%d", server.URL(), workerID, r),
					bodyReader,
				)
				if err != nil {
					errCh <- fmt.Errorf("worker %d req %d create error: %w", workerID, r, err)
					return
				}
				req.Header.Set("X-Worker-ID", strconv.Itoa(workerID))
				req.Header.Set("X-Seq-ID", strconv.Itoa(r))

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

				if !bytes.Equal(body, expectedBody) {
					errCh <- fmt.Errorf("worker %d req %d body mismatch: got %q, expected %q",
						workerID, r, string(body), string(expectedBody))
					return
				}

				completedReqs.Add(1)
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("H3Engine multiplexing error: %v", err)
	}

	assert.Equal(t, int64(numWorkers*reqsPerWorker), completedReqs.Load())
}

// -----------------------------------------------------------------------------
// Challenger Test 3: Rapid Context Cancellations Under Mixed Load (50 Healthy + 50 Chaotic)
// -----------------------------------------------------------------------------

func TestM1_Challenger_H3_RapidContextCancellations_MixedLoad(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/slow" {
			// Processing delay to allow in-flight cancellation
			select {
			case <-r.Context().Done():
				return
			case <-time.After(25 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("slow-response-ok"))
			}
			return
		}

		// Fast response for healthy streams
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Path", path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		healthyWorkers = 50
		chaoticWorkers = 50
		reqsPerWorker  = 6
	)

	var wg sync.WaitGroup
	healthyErrCh := make(chan error, healthyWorkers*reqsPerWorker)
	var healthyCompleted atomic.Int64
	var chaoticCancelled atomic.Int64

	// 1. Launch 50 healthy workers executing normal requests
	for w := 0; w < healthyWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for r := 0; r < reqsPerWorker; r++ {
				payload := fmt.Sprintf("healthy-worker-%d-req-%d", workerID, r)
				req, err := http.NewRequestWithContext(
					context.Background(),
					http.MethodPost,
					fmt.Sprintf("%s/fast/%d/%d", server.URL(), workerID, r),
					bytes.NewReader([]byte(payload)),
				)
				if err != nil {
					healthyErrCh <- fmt.Errorf("healthy w%d r%d create err: %w", workerID, r, err)
					return
				}

				resp, err := client.HTTP().Do(req)
				if err != nil {
					healthyErrCh <- fmt.Errorf("healthy w%d r%d execute err: %w", workerID, r, err)
					return
				}

				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					healthyErrCh <- fmt.Errorf("healthy w%d r%d read err: %w", workerID, r, err)
					return
				}

				if resp.StatusCode != http.StatusOK || string(body) != payload {
					healthyErrCh <- fmt.Errorf("healthy w%d r%d mismatch: status=%d body=%q",
						workerID, r, resp.StatusCode, string(body))
					return
				}

				healthyCompleted.Add(1)
			}
		}(w)
	}

	// 2. Concurrently launch 50 chaotic workers with aggressive cancellation timeouts
	for w := 0; w < chaoticWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Diverse cancellation timeouts: 0, 50us, 200us, 1ms, 5ms, 10ms
			delays := []time.Duration{
				0,
				50 * time.Microsecond,
				200 * time.Microsecond,
				1 * time.Millisecond,
				5 * time.Millisecond,
				10 * time.Millisecond,
			}

			for r := 0; r < reqsPerWorker; r++ {
				cancelDelay := delays[r%len(delays)]

				var ctx context.Context
				var cancel context.CancelFunc

				if cancelDelay == 0 {
					ctx, cancel = context.WithCancel(context.Background())
					cancel() // already cancelled
				} else {
					ctx, cancel = context.WithTimeout(context.Background(), cancelDelay)
				}

				req, err := http.NewRequestWithContext(
					ctx,
					http.MethodGet,
					fmt.Sprintf("%s/slow?w=%d&r=%d", server.URL(), workerID, r),
					nil,
				)
				if err != nil {
					cancel()
					continue
				}

				resp, err := client.HTTP().Do(req)
				if err != nil {
					// Expected failure due to cancellation
					chaoticCancelled.Add(1)
					cancel()
					continue
				}

				// If request somehow completed before cancellation fired
				_, _ = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()
			}
		}(w)
	}

	wg.Wait()
	close(healthyErrCh)

	// All healthy requests MUST have succeeded without error
	for err := range healthyErrCh {
		t.Errorf("Healthy worker suffered failure due to concurrent cancellations: %v", err)
	}

	assert.Equal(t, int64(healthyWorkers*reqsPerWorker), healthyCompleted.Load())
	assert.True(t, chaoticCancelled.Load() > 0, "chaotic workers should have witnessed cancellations")
}

// -----------------------------------------------------------------------------
// Challenger Test 4: Early Body Abort and Partial Stream Reads (50 Workers)
// -----------------------------------------------------------------------------

func TestM1_Challenger_H3_ResponseBody_EarlyAbortAndPartialReads(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	largePayload := make([]byte, 65536)
	_, _ = rand.Read(largePayload)

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(largePayload)
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const numWorkers = 50
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			resp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/partial")
			if err != nil {
				errCh <- fmt.Errorf("worker %d request failed: %w", workerID, err)
				return
			}

			// Read only 128 bytes then immediately close Body
			buf := make([]byte, 128)
			n, err := io.ReadFull(resp.Body, buf)
			if err != nil {
				_ = resp.Body.Close()
				errCh <- fmt.Errorf("worker %d read failed: %w", workerID, err)
				return
			}
			if n != 128 {
				_ = resp.Body.Close()
				errCh <- fmt.Errorf("worker %d short read: %d", workerID, n)
				return
			}

			// Close body prematurely without reading the rest of 65KB
			if err := resp.Body.Close(); err != nil {
				errCh <- fmt.Errorf("worker %d body close error: %w", workerID, err)
				return
			}

			// Redundant Close calls must be idempotent
			if err := resp.Body.Close(); err != nil {
				errCh <- fmt.Errorf("worker %d second body close error: %w", workerID, err)
				return
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Partial read / abort error: %v", err)
	}

	// Verify client is still completely functional after 50 partial stream aborts
	finalResp, err := client.Request(context.Background(), http.MethodGet, server.URL()+"/check-healthy")
	require.NoError(t, err)
	defer finalResp.Body.Close()
	assert.Equal(t, http.StatusOK, finalResp.StatusCode)
}

// -----------------------------------------------------------------------------
// Challenger Test 5: Engine Teardown / Close Under Active Concurrency
// -----------------------------------------------------------------------------

func TestM1_Challenger_H3Engine_CloseDuringActiveTransfers(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))

	const numWorkers = 30
	var wg sync.WaitGroup
	started := make(chan struct{})
	var activeCount atomic.Int32

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			if activeCount.Add(1) == numWorkers {
				close(started)
			}

			_, _ = client.Request(context.Background(), http.MethodGet, server.URL()+"/teardown")
		}(w)
	}

	// Wait until workers have dispatched requests
	<-started
	time.Sleep(20 * time.Millisecond)

	// Abruptly close client and engine while requests are in flight
	client.Close()

	// All workers must unblock without deadlocking
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// Success: all workers returned cleanly
	case <-time.After(3 * time.Second):
		t.Fatal("Deadlock: workers failed to return after client.Close()")
	}
}

// -----------------------------------------------------------------------------
// Challenger Test 6: Concurrent Header Integrity & Anti-Aliasing Under Heavy Load
// -----------------------------------------------------------------------------

func TestM1_Challenger_H3_HeaderIntegrity_ConcurrentHeavy(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(10*time.Second))()

	type validationResult struct {
		workerID int
		reqID    int
		err      error
	}

	resCh := make(chan validationResult, 2000)

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wWorker := r.Header.Get("X-Worker-ID")
		wReq := r.Header.Get("X-Req-ID")
		if wWorker == "" || wReq == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		wID, _ := strconv.Atoi(wWorker)
		rID, _ := strconv.Atoi(wReq)

		longHeaderKey := fmt.Sprintf("X-Long-Header-Name-Exceeding-Stack-Buffer-Limit-128-Bytes-Padding-Padding-Padding-Padding-Padding-Padding-Padding-Worker-%d-Req-%d-End", wID, rID)

		expectedHeaders := map[string]string{
			fmt.Sprintf("X-Short-A-%d-%d", wID, rID):                      fmt.Sprintf("val-a-%d-%d", wID, rID),
			fmt.Sprintf("X-Short-B-%d-%d", wID, rID):                      fmt.Sprintf("val-b-%d-%d", wID, rID),
			fmt.Sprintf("X-Short-C-%d-%d", wID, rID):                      fmt.Sprintf("val-c-%d-%d", wID, rID),
			fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha", wID, rID):            fmt.Sprintf("val-alpha-%d-%d", wID, rID),
			fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta", wID, rID):       fmt.Sprintf("val-alphabeta-%d-%d", wID, rID),
			fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta-Gamma", wID, rID): fmt.Sprintf("val-alphabetagamma-%d-%d", wID, rID),
			longHeaderKey:         fmt.Sprintf("long-val-%d-%d", wID, rID),
			"X-Mixed-Case-Header": fmt.Sprintf("mixed-val-%d-%d", wID, rID),
		}

		var checkErr error
		for k, expVal := range expectedHeaders {
			actualVal := r.Header.Get(k)
			if actualVal != expVal {
				checkErr = fmt.Errorf("worker %d req %d: header %q mismatch: expected %q, got %q (all headers: %#v)",
					wID, rID, k, expVal, actualVal, r.Header)
				break
			}
		}

		resCh <- validationResult{workerID: wID, reqID: rID, err: checkErr}
		if checkErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Subtest 1: aoni.Client + H3Engine under heavy concurrent requests
	t.Run("AoniClient_H3Engine", func(t *testing.T) {
		client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
		defer client.Close()

		const (
			numWorkers    = 50
			reqsPerWorker = 10
		)

		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers*reqsPerWorker)
		var completedReqs atomic.Int64

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for r := 0; r < reqsPerWorker; r++ {
					req, err := http.NewRequestWithContext(
						context.Background(),
						http.MethodGet,
						fmt.Sprintf("%s/headers-integrity-aoni/%d/%d", server.URL(), workerID, r),
						nil,
					)
					if err != nil {
						errCh <- fmt.Errorf("worker %d req %d req create err: %w", workerID, r, err)
						return
					}

					longKey := fmt.Sprintf("X-Long-Header-Name-Exceeding-Stack-Buffer-Limit-128-Bytes-Padding-Padding-Padding-Padding-Padding-Padding-Padding-Worker-%d-Req-%d-End", workerID, r)

					req.Header.Set("X-Worker-ID", strconv.Itoa(workerID))
					req.Header.Set("X-Req-ID", strconv.Itoa(r))
					req.Header.Set(fmt.Sprintf("X-Short-A-%d-%d", workerID, r), fmt.Sprintf("val-a-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Short-B-%d-%d", workerID, r), fmt.Sprintf("val-b-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Short-C-%d-%d", workerID, r), fmt.Sprintf("val-c-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha", workerID, r), fmt.Sprintf("val-alpha-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta", workerID, r), fmt.Sprintf("val-alphabeta-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta-Gamma", workerID, r), fmt.Sprintf("val-alphabetagamma-%d-%d", workerID, r))
					req.Header.Set(longKey, fmt.Sprintf("long-val-%d-%d", workerID, r))
					req.Header.Set("X-Mixed-Case-Header", fmt.Sprintf("mixed-val-%d-%d", workerID, r))

					resp, err := client.HTTP().Do(req)
					if err != nil {
						errCh <- fmt.Errorf("worker %d req %d client.Do err: %w", workerID, r, err)
						return
					}
					_ = resp.Body.Close()

					if resp.StatusCode != http.StatusOK {
						errCh <- fmt.Errorf("worker %d req %d bad status %d", workerID, r, resp.StatusCode)
						return
					}

					completedReqs.Add(1)
				}
			}(w)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("Client error: %v", err)
		}
		assert.Equal(t, int64(numWorkers*reqsPerWorker), completedReqs.Load())
	})

	// Subtest 2: mach/client/h3.ClientConn under heavy concurrent requests
	t.Run("Direct_ClientConn", func(t *testing.T) {
		ccServer := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wWorker := r.Header.Get("X-Worker-ID")
			wReq := r.Header.Get("X-Req-ID")
			if wWorker == "" || wReq == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			wID, _ := strconv.Atoi(wWorker)
			rID, _ := strconv.Atoi(wReq)

			longHeaderKey := fmt.Sprintf("X-Long-Header-Name-Exceeding-Stack-Buffer-Limit-128-Bytes-Padding-Padding-Padding-Padding-Padding-Padding-Padding-Worker-%d-Req-%d-End", wID, rID)

			expectedHeaders := map[string]string{
				fmt.Sprintf("X-Short-A-%d-%d", wID, rID):                      fmt.Sprintf("val-a-%d-%d", wID, rID),
				fmt.Sprintf("X-Short-B-%d-%d", wID, rID):                      fmt.Sprintf("val-b-%d-%d", wID, rID),
				fmt.Sprintf("X-Short-C-%d-%d", wID, rID):                      fmt.Sprintf("val-c-%d-%d", wID, rID),
				fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha", wID, rID):            fmt.Sprintf("val-alpha-%d-%d", wID, rID),
				fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta", wID, rID):       fmt.Sprintf("val-alphabeta-%d-%d", wID, rID),
				fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta-Gamma", wID, rID): fmt.Sprintf("val-alphabetagamma-%d-%d", wID, rID),
				longHeaderKey:         fmt.Sprintf("long-val-%d-%d", wID, rID),
				"X-Mixed-Case-Header": fmt.Sprintf("mixed-val-%d-%d", wID, rID),
			}

			for k, expVal := range expectedHeaders {
				actualVal := r.Header.Get(k)
				if actualVal != expVal {
					t.Errorf("worker %d req %d: header %q mismatch: expected %q, got %q", wID, rID, k, expVal, actualVal)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer ccServer.Close()

		ctx := context.Background()
		cc, err := ccServer.DialClientConn(ctx)
		require.NoError(t, err)
		defer cc.Close()

		const (
			numWorkers    = 50
			reqsPerWorker = 10
		)

		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers*reqsPerWorker)
		var completedReqs atomic.Int64

		for w := 50; w < 50+numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for r := 0; r < reqsPerWorker; r++ {
					req := machhttp.AcquireRequest()
					resp := machhttp.AcquireResponse()

					req.Header.SetMethod(http.MethodGet)
					req.SetRequestURI(fmt.Sprintf("%s/headers-integrity-cc/%d/%d", ccServer.URL(), workerID, r))
					req.Header.SetHost(ccServer.Addr())

					longKey := fmt.Sprintf("X-Long-Header-Name-Exceeding-Stack-Buffer-Limit-128-Bytes-Padding-Padding-Padding-Padding-Padding-Padding-Padding-Worker-%d-Req-%d-End", workerID, r)

					req.Header.Set("X-Worker-ID", strconv.Itoa(workerID))
					req.Header.Set("X-Req-ID", strconv.Itoa(r))
					req.Header.Set(fmt.Sprintf("X-Short-A-%d-%d", workerID, r), fmt.Sprintf("val-a-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Short-B-%d-%d", workerID, r), fmt.Sprintf("val-b-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Short-C-%d-%d", workerID, r), fmt.Sprintf("val-c-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha", workerID, r), fmt.Sprintf("val-alpha-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta", workerID, r), fmt.Sprintf("val-alphabeta-%d-%d", workerID, r))
					req.Header.Set(fmt.Sprintf("X-Prefix-Test-%d-%d-Alpha-Beta-Gamma", workerID, r), fmt.Sprintf("val-alphabetagamma-%d-%d", workerID, r))
					req.Header.Set(longKey, fmt.Sprintf("long-val-%d-%d", workerID, r))
					req.Header.Set("X-Mixed-Case-Header", fmt.Sprintf("mixed-val-%d-%d", workerID, r))

					_, err := cc.Do(ctx, req, resp, nil)
					if err != nil {
						machhttp.ReleaseRequest(req)
						machhttp.ReleaseResponse(resp)
						errCh <- fmt.Errorf("worker %d req %d cc.Do err: %w", workerID, r, err)
						return
					}

					if resp.StatusCode() != http.StatusOK {
						statusCode := resp.StatusCode()
						machhttp.ReleaseRequest(req)
						machhttp.ReleaseResponse(resp)
						errCh <- fmt.Errorf("worker %d req %d bad status %d", workerID, r, statusCode)
						return
					}

					machhttp.ReleaseRequest(req)
					machhttp.ReleaseResponse(resp)
					completedReqs.Add(1)
				}
			}(w)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("ClientConn error: %v", err)
		}
		assert.Equal(t, int64(numWorkers*reqsPerWorker), completedReqs.Load())
	})

	close(resCh)
	for res := range resCh {
		if res.err != nil {
			t.Fatalf("Server validation error: %v", res.err)
		}
	}
}
