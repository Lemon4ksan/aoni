// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resiliency

import (
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/resiliency/cache"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
)

type (
	// RetryCondition evaluates whether a failed transaction attempt should trigger a retry.
	RetryCondition = core.RetryCondition

	// RetryOverride overrides default client retry behavior for a specific request execution.
	RetryOverride = core.RetryOverride

	// FallbackFunc generates a synthetic fallback [aoni.Response] when a request execution permanently fails.
	FallbackFunc = core.FallbackFunc

	// CacheStore defines the persistence contract for HTTP response caching backends (e.g. Memory, Redis).
	CacheStore = cache.Store

	// ChallengeSolver delegates WAF/DDoS challenge page resolution (e.g. Cloudflare JS/Captcha)
	// to automated headless or external solver drivers.
	ChallengeSolver = challenge.Solver

	// ChallengeDetector determines whether an incoming HTTP response represents a WAF/DDoS challenge page.
	ChallengeDetector = challenge.Detector
)
