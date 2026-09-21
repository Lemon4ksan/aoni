// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"iter"
	"net/http"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/core"
)

// Universal Protocol Atoms.
//
// These core contracts bridge disparate networking paradigms (net/http, fasthttp, and gRPC)
// into a unified, type-safe, profile-driven architecture conforming strictly to RFC 9110.
type (
	// CookieJar defines a context-aware HTTP cookie storage interface supporting RFC 6265bis CHIPS.
	CookieJar = cookie.CookieJar

	// Request represents a unified, HTTP request abstraction conforming to RFC 9110.
	// It homogenizes standard net/http, fasthttp, and gRPC-Web request representations under a single,
	// high-throughput contract with zero heap allocations on hot paths.
	Request = core.Request

	// HeaderIterator is implemented by high-performance Request instances to support header traversal.
	HeaderIterator = core.HeaderIterator

	// Response represents a unified, high-performance HTTP response abstraction conforming to RFC 9110.
	// Provides zero-copy byte access, pooled memory recycling ([Response.Close]), and structured decoding facilities.
	Response = core.Response

	// RequestDoer is the universal execution contract for processing unified [Request] transactions.
	// It is implemented by [*Client], [*fast.Client], middleware decorators, load balancers, and transport bridges.
	RequestDoer = core.RequestDoer

	// HTTPRequester specifies an execution contract capable of executing parameterized HTTP requests.
	HTTPRequester = core.HTTPRequester

	// DoerFunc is an adapter allowing ordinary functions to satisfy the [RequestDoer] execution contract.
	DoerFunc = core.DoerFunc

	// ResponseDecoder deserializes an HTTP response body stream into a target Go data structure
	// based on the response Content-Type (e.g. JSON, XML, Protobuf, gRPC-Web).
	ResponseDecoder = core.ResponseDecoder

	// BaseResponse defines the envelope contract for structured API responses (e.g. status code, business errors).
	BaseResponse = core.BaseResponse

	// BaseResponseProvider yields an envelope instance used for structured response unwrapping.
	BaseResponseProvider = core.BaseResponseProvider

	// RequestFactory facilitates request object pooling across execution pipelines.
	RequestFactory = core.RequestFactory

	// QueryEncoder marshals custom structs or key-value pairs into standard URL query parameters.
	QueryEncoder = core.QueryEncoder

	// ProgressFunc reports real-time transfer progress for uploads and streaming downloads.
	ProgressFunc = core.ProgressFunc

	// RequestModifier is a composable, functional modifier applied to outgoing [Request] pipelines.
	RequestModifier = core.RequestModifier

	// Phase represents a specific discrete phase of the network request lifecycle.
	Phase = core.Phase

	// Error encapsulates a comprehensive, structured network failure across any transport layer.
	Error = core.Error

	// SoftErrorDetector inspects the response status, headers, and initial peeked body bytes
	// for application-layer soft errors (e.g. HTTP 200 OK containing an HTML login or error message).
	//
	// Non-Destructive Invariant:
	// The peek buffer is captured non-destructively. If detector returns a non-nil error,
	// request execution is aborted with that error without draining the body stream.
	SoftErrorDetector func(resp *http.Response, peek []byte) error

	// RetryCondition evaluates whether a failed transaction attempt should trigger a retry.
	RetryCondition = core.RetryCondition

	// RetryOverride overrides default client retry behavior for a specific request execution.
	RetryOverride = core.RetryOverride

	// RetryOptions configures backoff, jitter, and idempotency constraints for request retries.
	RetryOptions = core.RetryOptions

	// JitterStrategy defines randomized delay distribution algorithms for retries.
	JitterStrategy = core.JitterStrategy

	// FallbackFunc generates a synthetic fallback [Response] when a request execution permanently fails.
	FallbackFunc = core.FallbackFunc

	// Logger specifies the structured diagnostic logging interface.
	Logger = core.Logger

	// LoggerProvider provides access to the diagnostic Logger instance.
	LoggerProvider = core.LoggerProvider

	// BodyRewinder is implemented by request payloads supporting reproducible body re-reads.
	BodyRewinder = core.BodyRewinder

	// ContentTyper is implemented by request payloads or models that declare their own MIME Content-Type.
	ContentTyper = core.ContentTyper

	// BodyProvider is implemented by types capable of providing their own payload stream.
	BodyProvider = core.BodyProvider

	// DirectConsumer is implemented by response targets that consume the raw response stream directly.
	DirectConsumer = core.DirectConsumer

	// ModifierType specifies the discrete operation type of a [RequestModifier] value.
	ModifierType = core.ModifierType
)

const (
	// ModCustom executes a custom closure mutating the outgoing [Request].
	ModCustom = core.ModCustom

	// PhaseUnknown indicates the failure occurred outside tracked request phases.
	PhaseUnknown = core.PhaseUnknown

	// PhaseDNS indicates failure during domain name resolution.
	PhaseDNS = core.PhaseDNS

	// PhaseProxyConnect indicates failure during proxy tunnel establishment (SOCKS5 / HTTP CONNECT).
	PhaseProxyConnect = core.PhaseProxyConnect

	// PhaseTCPConnect indicates failure during raw TCP / QUIC socket dial.
	PhaseTCPConnect = core.PhaseTCPConnect

	// PhaseTLSHandshake indicates failure during TLS negotiation, certificate validation, or ECH exchange.
	PhaseTLSHandshake = core.PhaseTLSHandshake

	// PhaseSendHeaders indicates failure while framing and writing request headers.
	PhaseSendHeaders = core.PhaseSendHeaders

	// PhaseSendBody indicates failure while streaming the request body.
	PhaseSendBody = core.PhaseSendBody

	// PhaseWaitResponse indicates failure while waiting for initial response headers / TTFB (e.g. server timeout).
	PhaseWaitResponse = core.PhaseWaitResponse

	// PhaseReadBody indicates failure while reading the incoming response stream payload.
	PhaseReadBody = core.PhaseReadBody

	// JitterNone disables randomized delay distribution.
	JitterNone = core.JitterNone

	// JitterFull scales delay uniformly in [0, backoff].
	JitterFull = core.JitterFull

	// JitterEqual scales delay uniformly in [backoff/2, backoff].
	JitterEqual = core.JitterEqual
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

// UnwrapSeq returns an iterator yielding each unwrapped layer in a decorator chain.
// It stops when a layer cannot be unwrapped further or when a cycle is detected.
func UnwrapSeq(target any) iter.Seq[any] {
	return func(yield func(any) bool) {
		for curr := target; curr != nil; {
			if !yield(curr) {
				return
			}

			next := unwrapNext(curr)
			if next == nil || next == curr {
				return
			}

			curr = next
		}
	}
}

// UnwrapAs traverses nested decorator chains until an instance of target type T is discovered.
//
// Onion-Peeling Mechanics:
// In deeply layered architectures (e.g. RoundTripper -> Telemetry -> Retry -> CookieJar -> Transport),
// UnwrapAs unwinds layers recursively via type assertions,
// returning the inner instance and true if found, or the zero value of T and false.
func UnwrapAs[T any](target any) (T, bool) {
	for curr := range UnwrapSeq(target) {
		if typed, ok := curr.(T); ok {
			return typed, true
		}
	}

	return generic.Zero[T](), false
}

// UnwrapClient peels away decorator layers and returns the innermost [*Client].
func UnwrapClient(target any) *Client {
	if c, ok := target.(*Client); ok {
		return c
	}

	if unwrapped, ok := UnwrapAs[*Client](target); ok {
		return unwrapped
	}

	return nil
}

func unwrapNext(curr any) any {
	switch u := curr.(type) {
	case interface{ Unwrap() *Client }:
		return u.Unwrap()
	case interface{ Unwrap() *http.Client }:
		return u.Unwrap()
	case interface{ Unwrap() *http.Transport }:
		return u.Unwrap()
	case interface{ Unwrap() HTTPDoer }:
		return u.Unwrap()
	case interface{ Unwrap() RequestDoer }:
		return u.Unwrap()
	case interface{ Unwrap() http.RoundTripper }:
		return u.Unwrap()
	case interface{ Unwrap() any }:
		return u.Unwrap()
	case interface{ Unwrap() error }:
		return u.Unwrap()
	case interface{ Rest() any }:
		return u.Rest()
	case interface{ Requester() any }:
		return u.Requester()
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
