// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
)

func TestGetProxyOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		proxyURL    string
		expectSet   bool
		expectedURL string
	}{
		{
			name:        "proxy_override_set",
			proxyURL:    "http://proxy.local:8080",
			expectSet:   true,
			expectedURL: "http://proxy.local:8080",
		},
		{
			name:      "proxy_override_not_set",
			proxyURL:  "",
			expectSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)

			if tt.proxyURL != "" {
				mod.WithProxyOverride(tt.proxyURL)(aoni.NewStdRequest(req))
			}

			raw, ok := aoni.GetProxyOverride(req.Context()).Value()
			assert.Equal(t, tt.expectSet, ok)

			if tt.expectSet {
				assert.Equal(t, tt.expectedURL, raw)
			}
		})
	}
}

func TestProxyFuncWithOverride(t *testing.T) {
	t.Parallel()

	baseProxy := http.ProxyURL(&url.URL{Scheme: "http", Host: "global-proxy:8080"})

	tests := []struct {
		name         string
		base         func(*http.Request) (*url.URL, error)
		overrideURL  string
		expectedHost string
		expectNil    bool
	}{
		{
			name:         "prefer_request_override_over_base",
			base:         baseProxy,
			overrideURL:  "http://per-request-proxy:9090",
			expectedHost: "per-request-proxy:9090",
		},
		{
			name:         "fallback_to_base_when_no_override",
			base:         baseProxy,
			overrideURL:  "",
			expectedHost: "global-proxy:8080",
		},
		{
			name:        "nil_base_without_override_returns_nil",
			base:        nil,
			overrideURL: "",
			expectNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wrapped := aoni.ProxyFuncWithOverride(tt.base)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)

			if tt.overrideURL != "" {
				mod.WithProxyOverride(tt.overrideURL)(aoni.NewStdRequest(req))
			}

			u, err := wrapped(req)
			require.NoError(t, err)

			if tt.expectNil {
				assert.Nil(t, u)
			} else {
				require.NotNil(t, u)
				assert.Equal(t, tt.expectedHost, u.Host)
			}
		})
	}
}

func TestInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		applyMod    bool
		expectedVal bool
	}{
		{
			name:        "insecure_skip_verify_enabled",
			applyMod:    true,
			expectedVal: true,
		},
		{
			name:        "insecure_skip_verify_disabled",
			applyMod:    false,
			expectedVal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", nil)
			require.NoError(t, err)

			if tt.applyMod {
				mod.WithInsecureSkipVerify()(aoni.NewStdRequest(req))
			}

			assert.Equal(t, tt.expectedVal, aoni.GetInsecureSkipVerify(req.Context()))

			baseCfg := &tls.Config{MinVersion: tls.VersionTLS12}
			effectiveCfg := aoni.TLSConfigWithOverride(req.Context(), baseCfg)

			if tt.applyMod {
				require.NotNil(t, effectiveCfg)
				assert.True(t, effectiveCfg.InsecureSkipVerify)
			} else {
				assert.Same(t, baseCfg, effectiveCfg)
			}
		})
	}
}

func TestTCPDelay(t *testing.T) {
	t.Parallel()

	t.Run("get_and_swap_bounds", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			minDelay    time.Duration
			maxDelay    time.Duration
			expectSet   bool
			expectedMin time.Duration
			expectedMax time.Duration
		}{
			{
				name:        "valid_bounds",
				minDelay:    10 * time.Millisecond,
				maxDelay:    20 * time.Millisecond,
				expectSet:   true,
				expectedMin: 10 * time.Millisecond,
				expectedMax: 20 * time.Millisecond,
			},
			{
				name:        "swaps_reversed_bounds",
				minDelay:    50 * time.Millisecond,
				maxDelay:    10 * time.Millisecond,
				expectSet:   true,
				expectedMin: 10 * time.Millisecond,
				expectedMax: 50 * time.Millisecond,
			},
			{
				name:      "unset_delay",
				expectSet: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
				require.NoError(t, err)

				if tt.expectSet {
					mod.WithTCPDelay(tt.minDelay, tt.maxDelay)(aoni.NewStdRequest(req))
				}

				r, ok := aoni.GetTCPDelay(req.Context()).Value()
				assert.Equal(t, tt.expectSet, ok)

				if tt.expectSet {
					assert.Equal(t, tt.expectedMin, r.Min)
					assert.Equal(t, tt.expectedMax, r.Max)
				}
			})
		}
	})

	t.Run("apply_tcp_delay_execution", func(t *testing.T) {
		t.Parallel()

		t.Run("immediate_when_no_delay", func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)

			start := time.Now()
			err = aoni.ApplyTCPDelay(req.Context())
			require.NoError(t, err)
			assert.Less(t, time.Since(start), 10*time.Millisecond)
		})

		t.Run("delays_within_configured_bounds", func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)

			mod.WithTCPDelay(20*time.Millisecond, 30*time.Millisecond)(aoni.NewStdRequest(req))

			start := time.Now()
			err = aoni.ApplyTCPDelay(req.Context())
			elapsed := time.Since(start)

			require.NoError(t, err)
			assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
		})

		t.Run("aborts_on_context_cancellation", func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)

			mod.WithTCPDelay(500*time.Millisecond, 1*time.Second)(aoni.NewStdRequest(req))

			cancel()

			err = aoni.ApplyTCPDelay(req.Context())
			assert.ErrorIs(t, err, context.Canceled)
		})
	})
}

func TestConnMetadata(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	mod.WithConnMetadata("pool", "eu-west")(aoni.NewStdRequest(req))
	mod.WithConnMetadata("trace-id", "abc123")(aoni.NewStdRequest(req))

	tests := []struct {
		name        string
		key         string
		expectFound bool
		expectedVal any
	}{
		{
			name:        "existing_key_pool",
			key:         "pool",
			expectFound: true,
			expectedVal: "eu-west",
		},
		{
			name:        "existing_key_trace_id",
			key:         "trace-id",
			expectFound: true,
			expectedVal: "abc123",
		},
		{
			name:        "missing_key",
			key:         "nonexistent",
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			val, ok := aoni.GetConnMetadata(req.Context(), tt.key).Value()
			assert.Equal(t, tt.expectFound, ok)

			if tt.expectFound {
				assert.Equal(t, tt.expectedVal, val)
			}
		})
	}
}

func TestCacheTTL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		applyMod    bool
		ttl         time.Duration
		expectSet   bool
		expectedTTL time.Duration
	}{
		{
			name:        "cache_ttl_set",
			applyMod:    true,
			ttl:         5 * time.Minute,
			expectSet:   true,
			expectedTTL: 5 * time.Minute,
		},
		{
			name:      "cache_ttl_not_set",
			applyMod:  false,
			expectSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)

			if tt.applyMod {
				mod.WithCacheTTL(tt.ttl)(aoni.NewStdRequest(req))
			}

			d, ok := aoni.GetCacheTTL(req.Context()).Value()
			assert.Equal(t, tt.expectSet, ok)

			if tt.expectSet {
				assert.Equal(t, tt.expectedTTL, d)
			}
		})
	}
}

func TestResponseValidator(t *testing.T) {
	t.Parallel()

	t.Run("passes_when_header_valid", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Status", "ok")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(srv.Close)

		c := aoni.NewClient(nil, option.WithBaseURL(srv.URL))

		resp, err := request.Get(t.Context(), c, "/",
			mod.WithResponseValidator(func(resp *http.Response) error {
				if resp.Header.Get("X-Status") != "ok" {
					return errors.New("missing X-Status")
				}

				return nil
			}),
		)
		require.NoError(t, err)
		require.NotNil(t, resp)
		_ = resp.Body.Close()
	})

	t.Run("blocks_when_body_invalid", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Access Denied"))
		}))
		t.Cleanup(srv.Close)

		c := aoni.NewClient(nil, option.WithBaseURL(srv.URL))

		resp, err := request.Get(t.Context(), c, "/",
			mod.WithResponseValidator(func(resp *http.Response) error {
				body, _ := io.ReadAll(resp.Body)

				resp.Body = io.NopCloser(bytes.NewReader(body))
				if strings.Contains(string(body), "Access Denied") {
					return errors.New("aoni: access denied by validator")
				}

				return nil
			}),
		)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "access denied by validator")
	})
}

func TestRetryPolicyAndConditions(t *testing.T) {
	t.Parallel()

	t.Run("get_retry_override", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			override    aoni.RetryOverride
			applyMod    bool
			expectSet   bool
			expectedMax int
		}{
			{
				name:        "valid_retry_policy",
				override:    aoni.RetryOverride{MaxAttempts: 5, Backoff: 200 * time.Millisecond},
				applyMod:    true,
				expectSet:   true,
				expectedMax: 5,
			},
			{
				name:        "clamped_zero_max_attempts",
				override:    aoni.RetryOverride{MaxAttempts: 0},
				applyMod:    true,
				expectSet:   true,
				expectedMax: 1,
			},
			{
				name:      "unset_retry_policy",
				applyMod:  false,
				expectSet: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
				require.NoError(t, err)

				if tt.applyMod {
					mod.WithRetryPolicy(tt.override)(aoni.NewStdRequest(req))
				}

				o, ok := aoni.GetRetryOverride(req.Context()).Value()
				assert.Equal(t, tt.expectSet, ok)

				if tt.expectSet {
					assert.Equal(t, tt.expectedMax, o.MaxAttempts)
				}
			})
		}
	})

	t.Run("built_in_retry_conditions", func(t *testing.T) {
		t.Parallel()

		newResp := func(code int) aoni.Response {
			return aoni.NewStdResponse(&http.Response{StatusCode: code})
		}

		tests := []struct {
			name        string
			cond        aoni.RetryCondition
			resp        aoni.Response
			err         error
			expectRetry bool
		}{
			{
				name:        "retry_on_err_with_error",
				cond:        middleware.RetryOnErr(),
				err:         errors.New("network fail"),
				expectRetry: true,
			},
			{
				name:        "retry_on_err_without_error",
				cond:        middleware.RetryOnErr(),
				resp:        newResp(200),
				expectRetry: false,
			},
			{
				name:        "retry_transient_net_error",
				cond:        middleware.RetryOnTransientErrors(),
				err:         &net.OpError{Op: "dial", Err: errors.New("connection refused")},
				expectRetry: true,
			},
			{
				name:        "retry_rate_limit_429",
				cond:        middleware.RetryOnRateLimit(),
				resp:        newResp(http.StatusTooManyRequests),
				expectRetry: true,
			},
			{
				name:        "retry_rate_limit_200_ok",
				cond:        middleware.RetryOnRateLimit(),
				resp:        newResp(http.StatusOK),
				expectRetry: false,
			},
			{
				name:        "retry_gateway_502",
				cond:        middleware.RetryOnGatewayErrors(),
				resp:        newResp(http.StatusBadGateway),
				expectRetry: true,
			},
			{
				name:        "retry_gateway_200",
				cond:        middleware.RetryOnGatewayErrors(),
				resp:        newResp(http.StatusOK),
				expectRetry: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, tt.expectRetry, tt.cond(tt.resp, tt.err))
			})
		}
	})

	t.Run("combinators_or_and", func(t *testing.T) {
		t.Parallel()

		condTrue := func(_ aoni.Response, _ error) bool { return true }
		condFalse := func(_ aoni.Response, _ error) bool { return false }

		assert.True(t, aoni.Or(condTrue, condFalse)(nil, nil))
		assert.False(t, aoni.Or(condFalse, condFalse)(nil, nil))

		assert.True(t, aoni.And(condTrue, condTrue)(nil, nil))
		assert.False(t, aoni.And(condTrue, condFalse)(nil, nil))
	})
}

func TestRetryOnGRPCStatus(t *testing.T) {
	t.Parallel()

	cond := middleware.RetryOnGRPCStatus("14", "13")

	grpcErr14 := &decode.GRPCWebError{StatusCode: "14", Err: decode.ErrGRPCWebStatusError}
	grpcErr5 := &decode.GRPCWebError{StatusCode: "5", Err: decode.ErrGRPCWebStatusError}

	assert.True(t, cond(nil, grpcErr14))
	assert.False(t, cond(nil, grpcErr5))
	assert.False(t, cond(nil, errors.New("plain error")))
}

func TestFallbackFunctions(t *testing.T) {
	t.Parallel()

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	req := aoni.NewStdRequest(httpReq)

	t.Run("fallback_string", func(t *testing.T) {
		t.Parallel()

		fallback := aoni.FallbackString(http.StatusInternalServerError, "error fallback")
		resp, err := fallback(req, errors.New("original error"))
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode())
		body := resp.BodyBytes()
		assert.Equal(t, "error fallback", string(body))
	})

	t.Run("fallback_json", func(t *testing.T) {
		t.Parallel()

		payload := map[string]string{"error": "service unavailable"}
		fallback := aoni.FallbackJSON(http.StatusServiceUnavailable, payload)
		resp, err := fallback(req, errors.New("original error"))
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode())
		body := resp.BodyBytes()
		assert.Contains(t, string(body), "service unavailable")
	})
}

func TestResponseTrace(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := aoni.NewClient(nil, option.WithBaseURL(srv.URL))

	resp, err := request.Get(t.Context(), c, "/", mod.WithTraceContext())
	require.NoError(t, err)

	defer resp.Body.Close()

	extracted := aoni.ResponseTrace(resp)
	require.NotNil(t, extracted)
	assert.NotEmpty(t, extracted.JA4.JA4H)
}

func TestAsReplayable_MultipleReads(t *testing.T) {
	t.Parallel()

	originalData := "replayable stream content"
	rc := io.NopCloser(strings.NewReader(originalData))

	replayable := aoni.AsReplayable(rc)
	require.NotNil(t, replayable)

	// Read 1
	data1, err := io.ReadAll(replayable)
	require.NoError(t, err)
	assert.Equal(t, originalData, string(data1))

	// Reset & Read 2
	replayable.Reset()
	data2, err := io.ReadAll(replayable)
	require.NoError(t, err)
	assert.Equal(t, originalData, string(data2))
}

func TestContextModifiersAndRules(t *testing.T) {
	t.Parallel()

	t.Run("with_context_modifier", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		dummyMod := func(_ aoni.Request) {}

		ctx = aoni.WithContextModifier(ctx, dummyMod)
		mods := aoni.ContextModifiers(ctx)
		require.Len(t, mods, 1)

		assert.Nil(t, aoni.ContextModifiers(context.Background()))
	})

	t.Run("host_rewrite_rules", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		rules := map[string]string{"example.com": "127.0.0.1:8080"}
		mod.WithHostRewrite(rules)(aoni.NewStdRequest(req))

		extracted := aoni.HostRewriteRules(req.Context())
		require.NotNil(t, extracted)
		assert.Equal(t, "127.0.0.1:8080", extracted["example.com"])
	})

	t.Run("mark_modifier_error", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		testErr := errors.New("body error")
		aoni.MarkModifierError(req, testErr)

		cfg := aoni.GetRequestConfig(req.Context())
		require.NotNil(t, cfg)
		assert.ErrorIs(t, cfg.BodyError, testErr)
	})
}

func TestMod_WithVars_Validation(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/item/{id}", nil)
	require.NoError(t, err)

	// Odd number of arguments must trigger ErrInvalidPairCount in BodyError
	mod.WithVars("key_without_value")(aoni.NewStdRequest(req))

	cfg := aoni.GetRequestConfig(req.Context())
	require.NotNil(t, cfg)
	assert.ErrorIs(t, cfg.BodyError, mod.ErrInvalidPairCount)
}

func TestMiddleware_Recover(t *testing.T) {
	t.Parallel()

	var panicCaught any

	panicMiddleware := func(_ aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
			panic("simulated fatal pipeline panic")
		})
	}

	recoverMid := middleware.Recover(func(r any) {
		panicCaught = r
	})

	chained := middleware.Chain(aoni.NewClient(nil), recoverMid, panicMiddleware)

	req := aoni.NewStdRequest(&http.Request{})
	resp, err := chained.Do(req)

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered")
	assert.Equal(t, "simulated fatal pipeline panic", panicCaught)
}

func TestRetryOnTransientErrors(t *testing.T) {
	t.Parallel()

	cond := middleware.RetryOnTransientErrors()

	tests := []struct {
		name        string
		err         error
		expectRetry bool
	}{
		{
			name:        "nil_error",
			err:         nil,
			expectRetry: false,
		},
		{
			name:        "connection_refused_string",
			err:         errors.New("dial tcp: connection refused"),
			expectRetry: true,
		},
		{
			name:        "connection_reset_string",
			err:         errors.New("read tcp: connection reset by peer"),
			expectRetry: true,
		},
		{
			name:        "broken_pipe_string",
			err:         errors.New("write tcp: broken pipe"),
			expectRetry: true,
		},
		{
			name:        "unexpected_eof_not_transient",
			err:         io.ErrUnexpectedEOF,
			expectRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expectRetry, cond(nil, tt.err))
		})
	}
}
