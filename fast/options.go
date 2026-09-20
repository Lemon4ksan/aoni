// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"crypto/tls"

	coreh3 "github.com/lemon4ksan/mach/proto/h3"
)

// Option defines a functional option for configuring [fast.Client].
type Option func(*Client)

// WithTLSConfig configures the TLS client configuration for the fast client.
func WithTLSConfig(tlsConf *tls.Config) Option {
	return func(c *Client) {
		c.engine.TLSConfig = tlsConf
	}
}

// WithH2 enables and forces HTTP/2 transport in the fast client.
func WithH2() Option {
	return func(c *Client) {
		c.engine.EnableH2 = true
		c.engine.ForceH2 = true
	}
}

// WithH3 enables and forces HTTP/3 QUIC transport in the fast client.
func WithH3() Option {
	return func(c *Client) {
		c.engine.EnableH3 = true
		c.engine.ForceH3 = true
	}
}

// WithH3Settings configures HTTP/3 protocol parameters.
func WithH3Settings(settings *coreh3.Settings) Option {
	return func(c *Client) {
		c.engine.H3Settings = settings
	}
}
