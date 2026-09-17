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

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni"
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

	resp, err := client.R().Get("/")
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

func TestRetryBuilder_AllBackoffStrategiesAndJitters(t *testing.T) {
	t.Parallel()

	t.Run("linear_and_constant_backoff", func(t *testing.T) {
		t.Parallel()

		b1 := resiliency.NewRetry().
			LinearBackoff(10*time.Millisecond, 100*time.Millisecond, 20*time.Millisecond).
			WithFullJitter()
		opts1, _ := b1.ToOptions()
		assert.True(t, opts1.Jitter)

		b2 := resiliency.NewRetry().
			ConstantBackoff(50 * time.Millisecond).
			WithFullJitter()
		opts2, _ := b2.ToOptions()
		assert.True(t, opts2.Jitter)
		assert.Equal(t, 50*time.Millisecond, opts2.InitialBackoff)
	})

	t.Run("equal_and_decorrelated_jitter", func(t *testing.T) {
		t.Parallel()

		b1 := resiliency.NewRetry().
			ExponentialBackoff(10*time.Millisecond, 100*time.Millisecond).
			WithEqualJitter()
		opts1, _ := b1.ToOptions()
		assert.True(t, opts1.Jitter)

		b2 := resiliency.NewRetry().
			ExponentialBackoff(10*time.Millisecond, 100*time.Millisecond).
			WithDecorrelatedJitter()
		opts2, _ := b2.ToOptions()
		assert.True(t, opts2.Jitter)
	})

	t.Run("max_attempts_zero_sets_one", func(t *testing.T) {
		t.Parallel()

		b := resiliency.NewRetry().MaxAttempts(0)
		opts, _ := b.ToOptions()
		assert.Equal(t, uint32(1), opts.MaxAttempts)
	})

	t.Run("custom_backoff_and_honor_retry_after", func(t *testing.T) {
		t.Parallel()

		b := resiliency.NewRetry().
			WithBackoff(nil). // nil is safely ignored
			HonorRetryAfter(true, 15*time.Second).
			AllowedMethods("GET", "POST").
			OnGatewayErrors().
			OnGRPCStatus("UNAVAILABLE")

		opts, _ := b.ToOptions()
		assert.True(t, opts.HonorRetryAfter)
		assert.Equal(t, 15*time.Second, opts.MaxRetryAfter)
		assert.Equal(t, []string{"GET", "POST"}, opts.AllowedMethods)
	})

	t.Run("on_retry_and_to_override_and_build", func(t *testing.T) {
		t.Parallel()

		var retryHookCalled bool

		b := resiliency.NewRetry().
			MaxAttempts(2).
			OnRetry(func(_ uint32, _ error, _ time.Duration) {
				retryHookCalled = true
			})

		override := b.ToOverride()
		assert.Equal(t, 2, override.MaxAttempts)

		mw := b.Build()
		assert.NotNil(t, mw)

		opts, _ := b.ToOptions()
		if opts.OnRetry != nil {
			opts.OnRetry(1, nil, time.Millisecond)
		}

		assert.True(t, retryHookCalled)
	})
}

func TestResiliency_PredicatesAndAliases(t *testing.T) {
	t.Parallel()

	t.Run("or_and_predicates", func(t *testing.T) {
		t.Parallel()

		trueCond := func(_ aoni.Response, _ error) bool { return true }
		falseCond := func(_ aoni.Response, _ error) bool { return false }

		orComb := resiliency.Or(trueCond, falseCond)
		assert.True(t, orComb(nil, nil))

		andComb := resiliency.And(trueCond, falseCond)
		assert.False(t, andComb(nil, nil))
	})

	t.Run("fallback_helpers", func(t *testing.T) {
		t.Parallel()

		fbStr := resiliency.FallbackString(http.StatusOK, "hello")
		respStr, err := fbStr(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, respStr.StatusCode())

		fbJSON := resiliency.FallbackJSON(http.StatusOK, map[string]string{"k": "v"})
		respJSON, err := fbJSON(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, respJSON.StatusCode())
	})

	t.Run("contract_aliases", func(t *testing.T) {
		t.Parallel()

		cb := resiliency.NewCircuitBreaker(resiliency.CircuitBreakerConfig{
			FailureThreshold: 0.5,
			MinRequests:      3,
			Cooldown:         time.Minute,
		})
		assert.NotNil(t, cb)

		cg := resiliency.NewCoalesceGroup()
		assert.NotNil(t, cg)

		etagAuto := resiliency.NewETagAutomaton()
		assert.NotNil(t, etagAuto)
	})
}
