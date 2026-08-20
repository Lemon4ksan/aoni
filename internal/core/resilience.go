// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"time"
)

// RetryCondition evaluates whether a failed transaction attempt should trigger a retry.
type RetryCondition func(resp Response, err error) bool

// RetryOverride overrides default client retry behavior for a specific request execution.
type RetryOverride struct {
	Condition   RetryCondition
	Backoff     time.Duration
	MaxAttempts int
}

// FallbackFunc generates a synthetic fallback [Response] when a request execution permanently fails.
type FallbackFunc func(req Request, origErr error) (Response, error)
