// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/internal/core"
)

// Universal Protocol Atoms.
//
// These core contracts bridge disparate networking paradigms (net/http, fasthttp, and gRPC)
// into a unified, type-safe, profile-driven architecture conforming strictly to RFC 9110.
type (
	// Request represents a unified, zero-allocation HTTP request abstraction conforming to RFC 9110.
	// It homogenizes standard net/http, fasthttp, and gRPC-Web request representations under a single,
	// high-throughput contract with zero heap allocations on hot paths.
	Request = core.Request

	// HeaderIterator is implemented by high-performance Request instances to support zero-allocation header traversal.
	HeaderIterator = core.HeaderIterator

	// Response represents a unified, high-performance HTTP response abstraction conforming to RFC 9110.
	// Provides zero-copy byte access, pooled memory recycling ([Response.Close]), and structured decoding facilities.
	Response = core.Response

	// RequestDoer is the universal execution contract for processing unified [Request] transactions.
	// It is implemented by [*Client], [*fast.Client], middleware decorators, load balancers, and transport bridges.
	RequestDoer = core.RequestDoer

	// DoerFunc is an adapter allowing ordinary functions to satisfy the [RequestDoer] execution contract.
	DoerFunc = core.DoerFunc

	// ResponseDecoder deserializes an HTTP response body stream into a target Go data structure
	// based on the response Content-Type (e.g. JSON, XML, Protobuf, gRPC-Web).
	ResponseDecoder = core.ResponseDecoder

	// BaseResponse defines the envelope contract for structured API responses (e.g. status code, business errors).
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

	// SoftErrorDetector inspects the response status, headers, and initial peeked body bytes
	// for application-layer soft errors (e.g. HTTP 200 OK containing an HTML login or error message).
	//
	// Non-Destructive Invariant:
	// The peek buffer is captured non-destructively. If detector returns a non-nil error,
	// request execution is aborted with that error without draining the body stream.
	SoftErrorDetector func(resp *http.Response, peek []byte) error
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

	// Unwrapper is a protocol representing any wrapper entity capable of revealing its underlying wrapped object.
	Unwrapper[T any] interface {
		Unwrap() T
	}
)

// UnwrapAs traverses nested decorator chains until an instance of target type T is discovered.
//
// Onion-Peeling Mechanics:
// In deeply layered architectures (e.g. RoundTripper -> Telemetry -> Retry -> CookieJar -> Transport),
// UnwrapAs unwinds layers recursively via zero-allocation type assertions,
// returning the inner instance and true if found, or the zero value of T and false.
func UnwrapAs[T any](target any) (T, bool) {
	for curr := target; curr != nil; {
		if typed, ok := curr.(T); ok {
			return typed, true
		}

		next := unwrapNext(curr)
		if next == nil || next == curr {
			break
		}

		curr = next
	}

	return generic.Zero[T](), false
}

func unwrapNext(curr any) any {
	switch u := curr.(type) {
	case interface{ Unwrap() *Client }:
		return u.Unwrap()
	case interface{ Unwrap() HTTPDoer }:
		return u.Unwrap()
	case interface{ Unwrap() http.RoundTripper }:
		return u.Unwrap()
	case interface{ Unwrap() any }:
		return u.Unwrap()
	case interface{ Unwrap() error }:
		return u.Unwrap()
	default:
		return nil
	}
}

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
