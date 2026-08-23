// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package congestion

import (
	"github.com/lemon4ksan/aoni/internal/quic/internal/monotime"
)

// A Clock returns the current time
type Clock interface {
	Now() monotime.Time
}

// DefaultClock implements the Clock interface using the Go stdlib clock.
type DefaultClock struct{}

var _ Clock = DefaultClock{}

// Now gets the current time
func (DefaultClock) Now() monotime.Time {
	return monotime.Now()
}
