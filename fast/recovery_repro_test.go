// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/fast"
)

func TestRepro_Recovery421_RecursionBounded(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusMisdirectedRequest)
	}))
	defer ts.Close()

	client := fast.NewClient()
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetMethod("GET")
	req.SetURL(ts.URL)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusMisdirectedRequest, resp.StatusCode())
	// Should be bounded to initial attempt + at most 1 recovery attempt
	assert.LessOrEqual(t, attempts.Load(), int32(2))
}
