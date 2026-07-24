// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import "errors"

var (
	// ErrNilURL indicates an attempt to dispatch an HTTP request without a destination address.
	ErrNilURL = errors.New("fast: request URL is nil")

	// ErrTargetURLEmpty is returned when no target URL is provided for request execution.
	ErrTargetURLEmpty = errors.New("fast: target URL is empty")

	// ErrUTLSHandshakeFailed is returned when uTLS negotiation fails over a fasthttp socket.
	ErrUTLSHandshakeFailed = errors.New("fast: uTLS handshake failed")

	// ErrProxyConnectionFailed is returned when establishing an outbound proxy tunnel fails.
	ErrProxyConnectionFailed = errors.New("fast: proxy connection failed")

	// ErrMaxRedirectsExceeded is returned when the request halts because the maximum redirect threshold was reached.
	ErrMaxRedirectsExceeded = errors.New("fast: maximum redirects limit exceeded")

	// ErrCannotRewind is returned when a stream request body cannot be rewound for a 307/308 redirect.
	ErrCannotRewind = errors.New("fast: cannot rewind request body stream for redirect")
)
