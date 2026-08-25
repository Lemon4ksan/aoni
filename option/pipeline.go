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

// WithRetry attaches an automated retry and backoff policy constructed via [resiliency.RetryBuilder].
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

// WithMiddleware registers one or more [aoni.Middleware] decorators wrapping the client execution engine.
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

// WithPipeline returns an [aoni.ClientOption] setting default pipeline configurations.
func WithPipeline(pipe aoni.PipelineConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Pipeline = pipe
	}
}

// WithHedging returns an [aoni.ClientOption] configuring request hedging delay.
func WithHedging(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HedgingDelay = d
	}
}

// WithDynamicHedging returns an [aoni.ClientOption] configuring dynamic RTT-percentile request hedging.
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

// WithMaxResponseSize returns an [aoni.ClientOption] limiting response body consumption in bytes.
func WithMaxResponseSize(size int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MaxResponseSize = size
	}
}

// WithMultiReadBodyThreshold returns an [aoni.ClientOption] setting RAM buffering bounds for replayable reads.
func WithMultiReadBodyThreshold(threshold int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk returns an [aoni.ClientOption] disabling temporary file disk backing on multi-read buffer overflows.
func WithMultiReadDisableDisk(disable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadDisableDisk = disable
	}
}

// WithResponseValidator returns an [aoni.ClientOption] setting default response validation functions.
func WithResponseValidator(fn func(*http.Response) error) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ResponseValidator = fn
	}
}

// WithSoftErrorDetector returns an [aoni.ClientOption] registering callbacks that sniff initial
// response body bytes to catch application-level soft errors (e.g. 200 OK containing an HTML error)
// without draining or consuming the body stream.
func WithSoftErrorDetector(detectors ...aoni.SoftErrorDetector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.SoftErrorDetectors = append(cfg.Defaults.SoftErrorDetectors, detectors...)
	}
}

// WithCookieJar returns an [aoni.ClientOption] overriding default cookie storage.
func WithCookieJar(jar http.CookieJar) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CookieJar = jar
	}
}

// WithCookieJanitor returns an [aoni.ClientOption] enabling background cookie purging for [cookie.ProxyIsolatedJar].
func WithCookieJanitor(ctx context.Context, interval time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if pJar, ok := cfg.Engine.CookieJar.(*cookie.ProxyIsolatedJar); ok {
			pJar.StartJanitor(ctx, interval)
		}
	}
}

// WithCookieIndices enables selective cookie-based response caching, hashing only specified
// cookie names (e.g., "theme", "lang") into the cache key to maximize hit rates for static pages.
func WithCookieIndices(cookieNames ...string) aoni.ClientOption {
	return func(c *aoni.Config) {
		if c.Defaults.Pipeline.Cache == nil {
			c.Defaults.Pipeline.Cache = &aoni.CacheConfig{}
		}

		c.Defaults.Pipeline.Cache.CookieIndices = slices.Clone(cookieNames)
	}
}

// WithDuplicateRequestGuard enables ring-buffer duplicate request detection,
// triggering a diagnostic alert if the same URL is fetched within the window (e.g. 10s).
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

// WithDictionaryStore returns an [aoni.ClientOption] configuring a custom RFC 9842 dictionary cache.
func WithDictionaryStore(store *dict.Store) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DictionaryStore = store
	}
}

// WithDisableDictionaryCompression returns an [aoni.ClientOption] disabling RFC 9842 compression dictionary negotiation.
func WithDisableDictionaryCompression(disable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DisableDictionaryCompression = disable
	}
}
