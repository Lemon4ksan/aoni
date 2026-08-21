// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package backoff provides zero-allocation mathematical backoff calculators with full, equal,
// and decorrelated jitter distributions for resilience and retry pipelines.
// Core implementation is located in [github.com/lemon4ksan/foundation/sync/backoff].
package backoff

import (
	"time"

	fbackoff "github.com/lemon4ksan/foundation/sync/backoff"
)

// JitterMode defines the randomization algorithm applied to backoff intervals.
type JitterMode = fbackoff.JitterMode

const (
	JitterNone         = fbackoff.JitterNone
	JitterFull         = fbackoff.JitterFull
	JitterEqual        = fbackoff.JitterEqual
	JitterDecorrelated = fbackoff.JitterDecorrelated
)

// Strategy represents a stateful or stateless backoff delay generator.
type Strategy = fbackoff.Strategy

// ExponentialBackoff computes backoff growing by factor with an optional jitter distribution.
type ExponentialBackoff = fbackoff.ExponentialBackoff

// LinearBackoff computes backoff growing linearly by step on each attempt.
type LinearBackoff = fbackoff.LinearBackoff

// ConstantBackoff computes a fixed static delay with optional jitter.
type ConstantBackoff = fbackoff.ConstantBackoff

// NewExponential creates an [ExponentialBackoff] with reasonable defaults.
func NewExponential(initial, max time.Duration, factor float64) *ExponentialBackoff {
	return fbackoff.NewExponential(initial, max, factor)
}

// NewLinear creates a [LinearBackoff] calculator.
func NewLinear(initial, max, step time.Duration) *LinearBackoff {
	return fbackoff.NewLinear(initial, max, step)
}

// NewConstant creates a fixed static delay backoff.
func NewConstant(delay time.Duration) *ConstantBackoff {
	return fbackoff.NewConstant(delay)
}
