// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"time"

	"github.com/lemon4ksan/aoni/internal/requestutil"
)

// formatTimeout converts d into a PROTOCOL-HTTP2.md compliant "grpc-timeout" header string.
func formatTimeout(d time.Duration) string {
	return requestutil.FormatGRPCTimeout(d)
}
