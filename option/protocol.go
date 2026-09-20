// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"github.com/lemon4ksan/mach/client/h3"

	"github.com/lemon4ksan/aoni"
)

// WithHTTP2Config configures low-level HTTP/2 connection parameters (ping timeouts, strict errors).
func WithHTTP2Config(cfg aoni.HTTP2Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Engine.HTTP2Config = &cfg
	}
}

// WithH3 configures the client with an HTTP/3 execution engine.
func WithH3(opts ...aoni.H3Option) aoni.ClientOption {
	engine := aoni.NewH3Engine(opts...)

	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = engine
	}
}

// WithH3Conn configures the client to execute over an existing HTTP/3 ClientConn.
func WithH3Conn(conn *h3.ClientConn) aoni.ClientOption {
	engine := aoni.NewH3EngineFromConn(conn)

	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = engine
	}
}

// WithH3Engine configures the client with an existing H3Engine.
func WithH3Engine(engine *aoni.H3Engine) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = engine
	}
}
