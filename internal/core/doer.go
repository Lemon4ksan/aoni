// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"context"
	"net"
)

// GenericDoer represents an abstract execution engine for processing arbitrary Req types into Resp types.
type GenericDoer[Req any, Resp any] interface {
	Do(req Req) (Resp, error)
}

// RequestDoer represents an execution engine capable of processing unified [Request] objects.
// Satisfied by standard aoni.Client, fast.Client, and middleware chains.
type RequestDoer = GenericDoer[Request, Response]

// DoerFunc adapts a plain function matching the execution signature to [RequestDoer].
type DoerFunc func(req Request) (Response, error)

// Do executes the underlying function against req.
func (f DoerFunc) Do(req Request) (Response, error) {
	return f(req)
}

// WebSocketDialer is implemented by clients supporting raw TCP/TLS socket dialing
// for WebSocket upgrades over uTLS or HTTP/2 Extended CONNECT (RFC 8441).
type WebSocketDialer interface {
	DialTLSForWS(ctx context.Context, addr string) (net.Conn, error)
	DialPlainForWS(ctx context.Context, addr string) (net.Conn, error)
}
