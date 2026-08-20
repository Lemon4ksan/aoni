// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fingerprint

import (
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// SessionCache extends the uTLS ClientSessionCache contract to support proxy-isolated TLS 1.3 session tickets.
type SessionCache interface {
	utls.ClientSessionCache
	// SetProxyKey sets the active proxy isolation key.
	SetProxyKey(key string)
}

// ClientHelloSpecProvider generates or retrieves a uTLS ClientHelloSpec dynamically per handshake.
type ClientHelloSpecProvider interface {
	// ClientHelloSpec yields a custom uTLS ClientHelloSpec configuration.
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

// HTTP2Configurer customizes x/net/http2.Transport settings during connection setup.
type HTTP2Configurer interface {
	// ConfigureHTTP2 applies custom settings to an x/net/http2.Transport instance.
	ConfigureHTTP2(t *http2.Transport) error
}
