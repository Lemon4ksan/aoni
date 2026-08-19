// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resiliency_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/resiliency"
)

func TestRetryBuilder_FullPipeline(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	retryBuilder := resiliency.NewRetry().
		MaxAttempts(4).
		ConstantBackoff(10 * time.Millisecond).
		OnRateLimit().
		AutoIdempotencyKey()

	client := aoni.NewClient(
		ts.Client(),
		option.WithBaseURL(ts.URL),
		option.WithRetry(retryBuilder),
	)

	resp, err := fluent.R(client).Get("/")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestRetryBuilder_CustomConditions(t *testing.T) {
	t.Parallel()

	builder := resiliency.NewRetry().
		MaxAttempts(5).
		ExponentialBackoff(50*time.Millisecond, 500*time.Millisecond).
		WithFullJitter().
		OnStatus(http.StatusBadGateway, http.StatusServiceUnavailable).
		OnTransientErrors().
		OnCondition(func(resp aoni.Response, err error) bool {
			return errors.Is(err, context.DeadlineExceeded)
		})

	opts, cond := builder.ToOptions()
	assert.Equal(t, uint32(5), opts.MaxAttempts)
	assert.True(t, opts.Jitter)

	// Test conditions
	assert.True(t, cond(nil, context.DeadlineExceeded))
	assert.False(t, cond(nil, errors.New("business error")))
}
