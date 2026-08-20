// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/internal/core"
)

// Universal Protocol Atoms.
type (
	// Request represents a unified, zero-allocation HTTP request abstraction conforming to RFC 9110.
	// It unifies net/http, fasthttp, and gRPC-Web request payloads under a single thread-safe interface.
	Request = core.Request

	// Response represents a unified, high-performance HTTP response abstraction conforming to RFC 9110.
	// Provides zero-copy byte access, pooled memory recycling, and structured decoding facilities.
	Response = core.Response

	// RequestDoer is the universal execution contract for processing unified [Request] transactions.
	// Implemented by [Client], [fast.Client], middlewares, load balancers, and transport adapters.
	RequestDoer = core.RequestDoer

	// DoerFunc is an adapter allowing the use of ordinary functions as [RequestDoer] execution handlers.
	DoerFunc = core.DoerFunc

	// ResponseDecoder deserializes an HTTP response body stream into a target Go data structure.
	ResponseDecoder = core.ResponseDecoder

	// BaseResponse defines the envelope contract for structured API responses (e.g. status code, error message).
	BaseResponse = core.BaseResponse

	// BaseResponseProvider yields an envelope instance used for structured response unwrapping.
	BaseResponseProvider = core.BaseResponseProvider

	// RequestFactory facilitates zero-allocation request object pooling across execution pipelines.
	RequestFactory = core.RequestFactory

	// QueryEncoder marshals custom structs or key-value pairs into standard URL query parameters.
	QueryEncoder = core.QueryEncoder

	// ProgressFunc reports real-time transfer progress for uploads and streaming downloads.
	ProgressFunc = core.ProgressFunc

	// RequestModifier is a composable, zero-allocation functional modifier applied to outgoing [Request] pipelines.
	RequestModifier = core.RequestModifier
)

// Execution & Middleware Contracts.
type (
	// Middleware wraps a [RequestDoer] execution chain to inject cross-cutting behaviors
	// such as retries, circuit breakers, rate limiting, logging, caching, and challenge solving.
	Middleware func(next RequestDoer) RequestDoer

	// ClientOption specifies a functional configuration option for configuring [Client] instances.
	ClientOption = generic.Option[*Config]

	// WebSocketDialer establishes RFC 6455 / RFC 8441 WebSocket connections over TCP, TLS, or HTTP/2 Extended CONNECT.
	WebSocketDialer = core.WebSocketDialer

	// Configurable is a protocol representing any entity capable of immutably applying [ClientOption] layers.
	Configurable[T any] interface {
		With(opts ...ClientOption) T
	}
)

// ConfigureAs applies [ClientOption] layers to any target conforming to the [Configurable[T]] protocol.
func ConfigureAs[T any](target Configurable[T], opts ...ClientOption) T {
	return target.With(opts...)
}

// Configure applies [ClientOption] layers to any execution engine.
func Configure(doer any, opts ...ClientOption) RequestDoer {
	if len(opts) == 0 {
		if doer == nil {
			return NewClient(nil)
		}

		if rd, ok := doer.(RequestDoer); ok {
			return rd
		}

		return NewClient(doer)
	}

	if doer == nil {
		return NewClient(nil, opts...)
	}

	if c, ok := doer.(*Client); ok {
		return c.With(opts...)
	}

	if conf, ok := doer.(Configurable[RequestDoer]); ok {
		return conf.With(opts...)
	}

	type optionApplier interface {
		ApplyOptions(opts ...ClientOption) RequestDoer
	}
	if a, ok := doer.(optionApplier); ok {
		return a.ApplyOptions(opts...)
	}

	type withAny interface {
		With(opts ...ClientOption) any
	}
	if w, ok := doer.(withAny); ok {
		if res, ok := w.With(opts...).(RequestDoer); ok {
			return res
		}
	}

	return NewClient(doer, opts...)
}

var noopReleaseFunc = func() {}

// AcquireRequest obtains a pooled [Request] instance from doer if supported via [RequestFactory],
// or allocates a standard request wrapper. Returns the request and a release cleanup closure.
func AcquireRequest(doer any) (Request, func()) {
	if factory, ok := doer.(RequestFactory); ok {
		r := factory.AcquireRequest()
		return r, func() { factory.ReleaseRequest(r) }
	}

	return NewStdRequest(nil), noopReleaseFunc
}
