// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"github.com/lemon4ksan/aoni"
)

// WithHTTP2Config configures low-level HTTP/2 connection parameters (ping timeouts, strict errors).
func WithHTTP2Config(cfg aoni.HTTP2Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Engine.HTTP2Config = &cfg
	}
}
