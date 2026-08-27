// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/netutil/dict"
	"github.com/lemon4ksan/aoni/resiliency"
	"github.com/lemon4ksan/aoni/telemetry"
)

// WithRetry attaches an automated retry and backoff middleware constructed via [resiliency.RetryBuilder].
//
// Automatically handles transient network dropouts, HTTP 429 rate limits, and server 5xx errors.
//
// # Example
//
//	retryPolicy := resiliency.NewRetry().
//	    MaxAttempts(3).
//	    ExponentialBackoff(100*time.Millisecond, 2*time.Second).
//	    RetryOnStatus(http.StatusTooManyRequests, http.StatusBadGateway)
//
//	client := aoni.NewClient(nil,
//	    option.WithRetry(retryPolicy),
//	)
func WithRetry(builder *resiliency.RetryBuilder) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if builder != nil {
			var base any = cfg.Engine.CustomEngine
			if base == nil {
				base = http.DefaultClient
			}

			chained := middleware.Chain(base, builder.Build())
			cfg.Engine.CustomEngine = aoni.NewRequestDoerAdapter(chained)
		}
	}
}

// WithMiddleware registers one or more [aoni.Middleware] interceptors in the execution chain.
//
// Middlewares wrap the core transport and execute sequentially around each HTTP transaction.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithMiddleware(loggingMiddleware, rateLimitingMiddleware),
//	)
func WithMiddleware(middlewares ...aoni.Middleware) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if len(middlewares) == 0 {
			return
		}

		var base any = cfg.Engine.CustomEngine
		if base == nil {
			base = http.DefaultClient
		}

		chained := middleware.Chain(base, middlewares...)
		cfg.Engine.CustomEngine = aoni.NewRequestDoerAdapter(chained)
	}
}

// WithPipeline sets the default 5-stage pipeline processing parameters (compression, caching, validation).
func WithPipeline(pipe aoni.PipelineConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Pipeline = pipe
	}
}

// WithHedging configures speculative request hedging with a static delay.
//
// If the primary request does not produce first response bytes within delay d, a speculative secondary
// request is fired concurrently on an alternate connection, and the winner is returned.
func WithHedging(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HedgingDelay = d
	}
}

// WithDynamicHedging configures dynamic EWMA percentile request hedging (e.g. hedging at p95 latency).
func WithDynamicHedging(config *telemetry.DynamicHedgingConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if config == nil {
			dc := telemetry.DefaultDynamicHedgingConfig()
			cfg.Network.DynamicHedging = &dc
			return
		}

		cfg.Network.DynamicHedging = config
	}
}

// WithMaxResponseSize limits the maximum response body consumption in bytes to prevent out-of-memory DoS.
//
// Defaults to 10 MB (10 * 1024 * 1024). Set to -1 to disable limits.
func WithMaxResponseSize(size int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MaxResponseSize = size
	}
}

// WithMultiReadBodyThreshold sets the maximum in-memory buffer threshold for replayable response body reads.
func WithMultiReadBodyThreshold(threshold int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk disables spilling replayable response bodies to temporary disk files when RAM threshold is exceeded.
func WithMultiReadDisableDisk(disable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadDisableDisk = disable
	}
}

// WithResponseValidator registers a global response validator executed immediately after receiving headers.
//
// If the validator returns an error, decoding is aborted and the error is propagated to the caller.
func WithResponseValidator(fn func(*http.Response) error) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ResponseValidator = fn
	}
}

// WithSoftErrorDetector registers detectors that sniff initial response bytes to catch application-level errors
// (e.g. HTTP 200 containing `{ "error": "auth_failed" }`) without consuming the response stream.
func WithSoftErrorDetector(detectors ...aoni.SoftErrorDetector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.SoftErrorDetectors = append(cfg.Defaults.SoftErrorDetectors, detectors...)
	}
}

// WithCookieJar overrides the default cookie storage jar.
//
// Defaults to [cookie.ProxyIsolatedJar] for secure multi-tenant proxy isolation.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithCookieJar(cookie.NewProxyIsolatedJar()),
//	)
func WithCookieJar(jar http.CookieJar) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CookieJar = jar
	}
}

// WithCookieJanitor enables a background goroutine to periodically purge expired cookies from [cookie.ProxyIsolatedJar].
func WithCookieJanitor(ctx context.Context, interval time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if pJar, ok := cfg.Engine.CookieJar.(*cookie.ProxyIsolatedJar); ok {
			pJar.StartJanitor(ctx, interval)
		}
	}
}

// WithCookieIndices enables selective cookie-based response caching, including only specified
// cookie names in cache hash keys to maximize cache hit rates for anonymous users.
func WithCookieIndices(cookieNames ...string) aoni.ClientOption {
	return func(c *aoni.Config) {
		if c.Defaults.Pipeline.Cache == nil {
			c.Defaults.Pipeline.Cache = &aoni.CacheConfig{}
		}

		c.Defaults.Pipeline.Cache.CookieIndices = slices.Clone(cookieNames)
	}
}

// WithDuplicateRequestGuard enables ring-buffer duplicate request detection to detect accidental infinite request loops.
func WithDuplicateRequestGuard(window time.Duration, logger core.Logger) aoni.ClientOption {
	if window <= 0 {
		window = 10 * time.Second
	}

	guard := telemetry.NewDuplicateRequestGuard(128, window, func(method, rawURL string, elapsed time.Duration) {
		if logger != nil {
			logger.Warn("aoni telemetry: potential duplicate request loop detected",
				"method", method,
				"url", rawURL,
				"elapsed", elapsed,
			)
		}
	})

	return func(cfg *aoni.Config) {
		cfg.Defaults.BeforeRequest = append(cfg.Defaults.BeforeRequest, func(req *http.Request) {
			guard.CheckAndRecord(req.Method, req.URL.String())
		})
	}
}

// WithDictionaryStore configures a custom RFC 9842 Shared Compression Dictionary cache store.
//
// # RFC Compliance
//
// Conforms to RFC 9842 (Compression Dictionary Transport).
func WithDictionaryStore(store *dict.Store) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DictionaryStore = store
	}
}

// WithDisableDictionaryCompression disables RFC 9842 compression dictionary negotiation ("Available-Dictionary" / "Use-As-Dictionary").
func WithDisableDictionaryCompression(disable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DisableDictionaryCompression = disable
	}
}
