// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"crypto/tls"

	"github.com/lemon4ksan/aoni"
	coreh3 "github.com/lemon4ksan/mach/proto/h3"
)

// Option defines a functional option for configuring [fast.Client].
// It aliases [aoni.ClientOption] to provide seamless composability across fast and standard clients.
type Option = aoni.ClientOption

// WithTLSConfig configures the TLS client configuration for the fast client.
func WithTLSConfig(tlsConf *tls.Config) Option {
	return func(c *aoni.Config) {
		c.Network.TLSConfig = tlsConf
	}
}

// WithH2 enables and forces HTTP/2 transport in the fast client.
func WithH2() Option {
	return func(c *aoni.Config) {
		c.Engine.EnableH2 = true
	}
}

// WithH3 enables and forces HTTP/3 QUIC transport in the fast client.
func WithH3() Option {
	return func(c *aoni.Config) {
		c.Engine.EnableH3 = true
	}
}

// WithH3Settings configures HTTP/3 protocol parameters.
func WithH3Settings(settings *coreh3.Settings) Option {
	return func(c *aoni.Config) {
		c.Engine.H3Settings = settings
	}
}
