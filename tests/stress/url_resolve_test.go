// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stress

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/testutil"
)

// TestURLResolutionStress ensures that BaseURL resolution using JoinPath
// works correctly and efficiently under high concurrency without data races or memory corruption.
func TestURLResolutionStress(t *testing.T) {
	const workers = 50
	const requestsPerWorker = 500

	var hits int32

	mock := testutil.NewMockEngine()
	mock.On("GET", "/api/v2/listings").Reply(http.StatusOK)

	// BaseURL has a trailing slash for some calls, no trailing slash for others.
	// Both should resolve to the same path correctly with JoinPath logic.
	baseURLs := []string{
		"http://example.com/api",
		"http://example.com/api/",
	}

	for _, bURL := range baseURLs {
		t.Run(fmt.Sprintf("BaseURL=%s", bURL), func(t *testing.T) {
			client := aoni.NewClient(mock, option.WithBaseURL(bURL))

			var wg sync.WaitGroup
			start := time.Now()

			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for j := 0; j < requestsPerWorker; j++ {
						// Paths with and without leading slash
						path := "v2/listings"
						if j%2 == 0 {
							path = "/v2/listings"
						}

						resp, err := client.Get(context.Background(), path)
						if err != nil {
							t.Errorf("Worker %d request failed: %v", workerID, err)
							return
						}

						if resp.StatusCode != http.StatusOK {
							t.Errorf("Worker %d got unexpected status %d for path %s", workerID, resp.StatusCode, path)
						}
						atomic.AddInt32(&hits, 1)
						_ = resp.Body.Close()
					}
				}(i)
			}

			wg.Wait()
			t.Logf("Stress test completed in %v", time.Since(start))
		})
	}

	expectedHits := int32(workers * requestsPerWorker * len(baseURLs))
	actualHits := atomic.LoadInt32(&hits)
	if actualHits != expectedHits {
		t.Errorf("Expected %d hits, got %d", expectedHits, actualHits)
	}
}
