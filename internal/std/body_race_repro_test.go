// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package std_test

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"

	"github.com/lemon4ksan/aoni/internal/std"
)

func TestRepro_StdResponse_BodyBytesConcurrentRace(t *testing.T) {
	t.Parallel()

	rawPayload := []byte("concurrency-test-payload-bytes")
	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(rawPayload)),
	}

	resp := std.NewResponse(httpResp)
	defer std.ReleaseResponse(resp)

	const concurrency = 20

	var wg sync.WaitGroup

	results := make([][]byte, concurrency)

	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			results[idx] = resp.BodyBytes()
		}(i)
	}

	wg.Wait()

	for _, b := range results {
		assert.Equal(t, string(rawPayload), string(b))
	}
}

func TestRepro_StdResponse_DoubleReleaseIdempotence(t *testing.T) {
	t.Parallel()

	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader([]byte("test"))),
	}

	resp := std.NewResponse(httpResp)
	// First release via Close
	assert.NoError(t, resp.Close())
	// Second release via ReleaseResponse (must be a safe no-op, not corrupting the pool)
	std.ReleaseResponse(resp)
	// Third release via Close again
	assert.NoError(t, resp.Close())
}

func TestRepro_StdRequest_DoubleReleaseIdempotence(t *testing.T) {
	t.Parallel()

	req := std.NewRequest(nil)
	std.ReleaseRequest(req)
	// Second release must be a safe no-op
	std.ReleaseRequest(req)
}
