// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"net/url"

	"github.com/lemon4ksan/aoni/foundation/ringbuf"
)

// Protocol represents the underlying L7 transport protocol version.
type Protocol int

const (
	// ProtocolHTTP1 represents HTTP/1.1 over TCP/TLS.
	ProtocolHTTP1 Protocol = iota
	// ProtocolHTTP2 represents HTTP/2 over TCP/TLS (H2).
	ProtocolHTTP2
	// ProtocolHTTP3 represents HTTP/3 over QUIC/UDP (H3).
	ProtocolHTTP3
)

// Dispatcher determines the protocol version and routing strategy for outbound requests.
type Dispatcher struct {
	altSvcCache *AltSvcCache
	queue       *ringbuf.RingBuffer[any]
}

// NewDispatcher constructs a [Dispatcher].
func NewDispatcher(altSvc *AltSvcCache) *Dispatcher {
	if altSvc == nil {
		altSvc = NewAltSvcCache()
	}

	return &Dispatcher{
		altSvcCache: altSvc,
		queue:       ringbuf.NewRingBuffer[any](1024),
	}
}

// ResolveProtocol selects the optimal protocol for a target URL taking Alt-Svc cache into account.
func (d *Dispatcher) ResolveProtocol(targetURL *url.URL, forcedProtocol string) Protocol {
	if forcedProtocol != "" {
		switch forcedProtocol {
		case "h3", "http3", "quic":
			return ProtocolHTTP3
		case "h2", "http2":
			return ProtocolHTTP2
		case "http/1.1", "h1":
			return ProtocolHTTP1
		}
	}

	if targetURL == nil {
		return ProtocolHTTP1
	}

	if targetURL.Scheme == "http" {
		return ProtocolHTTP1
	}

	if d.altSvcCache != nil && d.altSvcCache.HasH3Support(targetURL.Host) {
		return ProtocolHTTP3
	}

	return ProtocolHTTP2
}
