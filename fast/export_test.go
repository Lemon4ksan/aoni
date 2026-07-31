// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
)

// ResolveALPNModeForTest exports resolveALPNMode for external package tests.
func ResolveALPNModeForTest(ctx context.Context, cfg *aoni.Config, req *fasthttp.Request) string {
	return resolveALPNMode(ctx, cfg, req)
}
