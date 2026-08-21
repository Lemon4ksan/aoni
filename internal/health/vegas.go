// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package health

import (
	"github.com/lemon4ksan/foundation/sync/limiter"
)

// VegasEngine is a pure control-theory TCP Vegas adaptive concurrency regulator.
// Core implementation is located in [github.com/lemon4ksan/foundation/sync/limiter].
type VegasEngine = limiter.VegasEngine

// NewVegasEngine initializes a [VegasEngine] with bounds and thresholds.
func NewVegasEngine(alpha, beta float64, initialCwnd, maxCwnd int) *VegasEngine {
	return limiter.NewVegasEngine(alpha, beta, initialCwnd, maxCwnd)
}
