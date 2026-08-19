// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resiliency

import (
	"github.com/lemon4ksan/aoni/middleware"
)

var (
	// Or combines multiple [RetryCondition] predicates, returning true if ANY condition is satisfied.
	Or = middleware.Or

	// And combines multiple [RetryCondition] predicates, returning true if ALL conditions are satisfied.
	And = middleware.And

	// FallbackString returns a [FallbackFunc] producing a static plaintext HTTP response.
	FallbackString = middleware.FallbackString

	// FallbackJSON returns a [FallbackFunc] serializing payload as JSON in a synthetic HTTP response.
	FallbackJSON = middleware.FallbackJSON
)
