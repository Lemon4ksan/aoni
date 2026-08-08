// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Request defines the unified, engine-agnostic HTTP request interface.
//
// It provides both string-based and byte-based accessors to ensure zero-allocation
// operations for high-performance engines while remaining 100% compatible with standard net/http.
type Request = pipeline.Request

// Response defines the unified, engine-agnostic HTTP response interface.
type Response = pipeline.Response

// ResponseDecoder defines the contract for unmarshaling response payload streams into Go structures.
type ResponseDecoder = pipeline.ResponseDecoder

// RequestDoer represents an engine capable of executing unified [Request] objects,
// satisfied by both [aoni.Client] and [fast.Client].
type RequestDoer = pipeline.RequestDoer

// DoerFunc adapts a plain function matching the request execution signature to the [RequestDoer] interface.
type DoerFunc func(req Request) (Response, error)

func (f DoerFunc) Do(req Request) (Response, error) {
	return f(req)
}

// RetryCondition evaluates whether a failed request attempt should trigger a retry.
type RetryCondition = pipeline.RetryCondition

// RetryOverride overrides the default retry behavior for a specific request.
type RetryOverride = pipeline.RetryOverride

// FallbackFunc generates an alternative [Response] when a request fails.
type FallbackFunc = pipeline.FallbackFunc

// BaseResponse is the interface implemented by all response types returned by the client.
type BaseResponse = pipeline.BaseResponse

// RequestModifier represents a functional hook that mutates an outgoing [Request] contract prior to dispatch.
type RequestModifier = generic.Option[Request]

// ClientOption represents a functional option that configures [Client] initialization or cloning.
type ClientOption generic.Option[*Config]

// Middleware decorates a [RequestDoer] with request and response interception logic.
type Middleware func(next RequestDoer) RequestDoer

// Configure applies [ClientOption] layers to any engine (RequestDoer, *Client, *fast.Client, or HTTPDoer).
//
// If doer natively supports option configuration (such as [*Client] or [*fast.Client]),
// options are applied directly to the underlying engine without wrapping.
// If doer is nil or a raw engine without option support, instantiates a configured [*Client].
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

	type withRequestDoer interface {
		With(opts ...ClientOption) RequestDoer
	}
	if w, ok := doer.(withRequestDoer); ok {
		return w.With(opts...)
	}

	type withClient interface {
		With(opts ...ClientOption) *Client
	}
	if w, ok := doer.(withClient); ok {
		return w.With(opts...)
	}

	val := reflect.ValueOf(doer)

	method := val.MethodByName("With")
	if method.IsValid() && method.Type().NumIn() == 1 && method.Type().IsVariadic() {
		args := []reflect.Value{reflect.ValueOf(opts)}

		out := method.CallSlice(args)
		if len(out) == 1 {
			if res, ok := out[0].Interface().(RequestDoer); ok {
				return res
			}
		}
	}

	return NewClient(doer, opts...)
}

// RequestFactory is implemented by engines capable of pooling their own high-performance Request instances.
type RequestFactory interface {
	AcquireRequest() Request
	ReleaseRequest(req Request)
}

// BaseResponseProvider provides a [BaseResponse] model for structured unwrapping.
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// QueryEncoder marshals arbitrary structures or maps into [url.Values].
type QueryEncoder func(any) (url.Values, error)

// WSDialer is implemented by clients that support raw TCP/TLS socket dialing for WebSocket upgrades.
type WSDialer interface {
	DialTLSForWS(ctx context.Context, addr string) (net.Conn, error)
	DialPlainForWS(ctx context.Context, addr string) (net.Conn, error)
}

// DNSResolver defines the hostname-to-IP lookup resolution contract.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SocketController directly configures underlying TCP sockets before SYN packets are written.
type SocketController interface {
	Control(fd uintptr, network, address string) error
}

// SessionCache extends the uTLS session caching contract to support proxy-isolated TLS tickets.
type SessionCache interface {
	utls.ClientSessionCache
	SetProxyKey(key string)
}

// ClientHelloSpecProvider generates or retrieves a uTLS ClientHelloSpec dynamically.
type ClientHelloSpecProvider interface {
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

// HTTP2Configurer customizes the [golang.org/x/net/http2.Transport] instance.
type HTTP2Configurer interface {
	ConfigureHTTP2(t *http2.Transport) error
}

// ProgressFunc represents a callback triggered periodically to monitor stream transfer progress.
type ProgressFunc = io.ProgressFunc

// CacheStore defines the persistence contract for response caching backends.
type CacheStore interface {
	Get(ctx context.Context, key any) ([]byte, error)
	Set(ctx context.Context, key any, val []byte, ttl time.Duration) error
}

// ChallengeSolver delegates WAF challenge resolution to an automated external driver.
type ChallengeSolver interface {
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

// ChallengeDetector decides whether an incoming HTTP response represents a WAF challenge.
type ChallengeDetector func(resp *http.Response) (bool, error)

// TrafficInspector captures and records request traces and headers for diagnostics.
type TrafficInspector interface {
	Capture(req *http.Request, resp *http.Response, err error, traceInfo *telemetry.TraceInfo)
}

// HARTracker records HTTP transactions into HAR session logs.
type HARTracker interface {
	Record(req *http.Request, resp *http.Response, startTime time.Time, duration int64)
}

// Logger specifies the structured diagnostic logging interface.
type Logger interface {
	Debug(msg string, args ...any)
	DebugContext(ctx context.Context, msg string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	Warn(msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	Error(msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// LoggerProvider provides access to the diagnostic Logger instance.
type LoggerProvider interface {
	Logger() Logger
}
