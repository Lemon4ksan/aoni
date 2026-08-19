// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inbound

import (
	"crypto/tls"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/tunnel/ssh/ca"
)

// Option configures an inbound proxy Server instance.
type Option func(*Server) error

// WithListenAddr sets the listening network address (e.g. "127.0.0.1:1080").
func WithListenAddr(addr string) Option {
	return func(s *Server) error {
		s.Addr = addr
		return nil
	}
}

// WithEngine sets the outbound [aoni.RequestDoer] used for L7 MITM requests and plain HTTP proxying.
func WithEngine(engine aoni.RequestDoer) Option {
	return func(s *Server) error {
		s.Engine = engine
		return nil
	}
}

// WithMITM enables or disables L7 TLS MITM interception and dynamic certificate generation.
func WithMITM(enabled bool) Option {
	return func(s *Server) error {
		s.EnableMITM = enabled
		return nil
	}
}

// WithCA sets a custom Certificate Authority used for dynamic MITM certificate signing.
func WithCA(ca *ca.CA) Option {
	return func(s *Server) error {
		s.CA = ca
		return nil
	}
}

// WithRootCACertificate sets a root CA TLS certificate for dynamic MITM certificate signing.
func WithRootCACertificate(cert tls.Certificate) Option {
	return func(s *Server) error {
		s.RootCACert = &cert
		return nil
	}
}

// WithAuthenticator sets a credential verification callback for SOCKS5/HTTP Proxy-Authorization.
func WithAuthenticator(auth func(username, password string) bool) Option {
	return func(s *Server) error {
		s.Auth = auth
		return nil
	}
}
