// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"strconv"
	"time"
)

// formatTimeout converts d into a PROTOCOL-HTTP2.md compliant "grpc-timeout" header string.
func formatTimeout(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}

	switch {
	case d < time.Microsecond:
		return strconv.FormatInt(d.Nanoseconds(), 10) + "n"
	case d < time.Millisecond:
		return strconv.FormatInt(d.Microseconds(), 10) + "u"
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "m"
	case d < time.Minute:
		return strconv.FormatInt(int64(d.Seconds()), 10) + "S"
	case d < time.Hour:
		return strconv.FormatInt(int64(d.Minutes()), 10) + "M"
	default:
		return strconv.FormatInt(int64(d.Hours()), 10) + "H"
	}
}
