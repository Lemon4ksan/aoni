// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Request defines the unified, engine-agnostic HTTP request contract.
// It abstracts standard *http.Request and fasthttp.Request into a single,
// high-performance interface supporting both string and zero-alloc byte accessors.
//
// Thread Safety & Mutability:
// Request instances are NOT thread-safe. They must be configured sequentially
// (e.g. via RequestModifier functional chains) before being passed to an execution engine.
// Once dispatched, the request MUST NOT be mutated concurrently.
//
// Memory Lifetime:
// Byte-slice accessors (e.g., BodyBytes(), HeaderBytes()) may return pointers to
// volatile internal memory buffers allocated from memory pools. Callers MUST NOT
// retain or mutate these slices beyond the lifespan of the request execution cycle.
type Request = pipeline.Request

// Response defines the unified, engine-agnostic HTTP response contract.
// It manages status codes, response headers, HTTP/2 & HTTP/3 trailers, and body streams.
//
// Resource Cleanup Invariant:
// Callers MUST invoke Close() on every returned Response instance (or read and close
// its BodyStream()) to drain pending payload frames and return underlying TCP/QUIC
// sockets back to connection keep-alive pools. Failure to close responses causes
// connection pool exhaustion and socket leaks.
//
// Zero-Allocation Buffer Access:
// UnsafeBodyBytes() yields direct access to internal volatile response buffers without
// memory copying. Callers MUST NOT retain or mutate this slice after calling Close().
type Response = pipeline.Response

// ResponseDecoder defines the contract for unmarshaling response payload streams into Go structures.
// Implementations handle format-specific parsing (JSON, Protobuf, gRPC-Web, XML, YAML).
type ResponseDecoder = pipeline.ResponseDecoder

// RequestDoer represents an execution engine capable of processing unified [Request] objects.
// Satisfied by both standard [aoni.Client] and high-performance [fast.Client].
//
// Thread Safety Requirement:
// Implementations MUST be fully thread-safe and safe for concurrent invocation
// across multiple goroutines.
type RequestDoer = pipeline.RequestDoer[Request, Response]

// DoerFunc adapts a plain function matching the execution signature to [RequestDoer].
type DoerFunc func(req Request) (Response, error)

// Do executes the underlying function against req.
func (f DoerFunc) Do(req Request) (Response, error) {
	return f(req)
}

// RetryCondition evaluates whether a failed transaction attempt should trigger a retry.
// Inspects the response status code, transport errors, or gRPC status trailers.
type RetryCondition = pipeline.RetryCondition

// RetryOverride overrides default client retry behavior for a specific request execution.
// Inspects the response status code, transport errors, or gRPC status trailers.
type RetryOverride = pipeline.RetryOverride

// FallbackFunc generates a synthetic fallback [Response] when a request execution permanently fails.
type FallbackFunc = pipeline.FallbackFunc

// BaseResponse is the interface implemented by structured envelope responses
// (e.g., API response wrappers containing { "data": ..., "error": ... }).
type BaseResponse = pipeline.BaseResponse

// ProgressFunc is a callback invoked periodically to monitor stream upload or download progress.
//   - current: cumulative bytes transferred so far.
//   - total: total expected bytes from Content-Length (-1 if unknown).
type ProgressFunc = io.ProgressFunc

// ModifierType specifies the discrete operation type of a [RequestModifier] value.
type ModifierType = pipeline.ModifierType

const (
	ModNone       = pipeline.ModNone
	ModHeader     = pipeline.ModHeader
	ModHeaderAdd  = pipeline.ModHeaderAdd
	ModQuery      = pipeline.ModQuery
	ModQueryAdd   = pipeline.ModQueryAdd
	ModBearer     = pipeline.ModBearer
	ModBasicAuth  = pipeline.ModBasicAuth
	ModBodyBytes  = pipeline.ModBodyBytes
	ModBodyStream = pipeline.ModBodyStream
	ModCustom     = pipeline.ModCustom
)

// RequestModifier represents a zero-allocation value-based modification payload.
type RequestModifier = pipeline.RequestModifier

// ClientOption represents a functional option configuring immutable [Client] initialization.
type ClientOption generic.Option[*Config]

// Middleware decorates a [RequestDoer] with request/response interception logic
// (e.g. Rate Limiting, Retries, Circuit Breaking, Logging, Chaos Engineering).
type Middleware func(next RequestDoer) RequestDoer

// Configurable is implemented by clients capable of cloning themselves with new options.
type Configurable interface {
	// With produces a cloned [RequestDoer] with options applied.
	With(opts ...ClientOption) RequestDoer
}

// Configure applies [ClientOption] layers to any execution engine (RequestDoer, *Client, *fast.Client, or HTTPDoer).
//
// If doer natively implements [Configurable] or supports option configuration,
// options are applied directly to the underlying engine without wrapping overhead.
// If doer is nil or a raw engine without option support, instantiates a configured [*Client].
//
// Configure is safe for concurrent use by multiple goroutines.
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

	type optionApplier interface {
		ApplyOptions(opts ...ClientOption) RequestDoer
	}
	if a, ok := doer.(optionApplier); ok {
		return a.ApplyOptions(opts...)
	}

	type withRequestDoer interface {
		With(opts ...ClientOption) RequestDoer
	}
	if w, ok := doer.(withRequestDoer); ok {
		return w.With(opts...)
	}

	return NewClient(doer, opts...)
}

// RequestFactory is implemented by engines capable of pooling their own high-performance Request instances
// to minimize GC allocation overhead.
type RequestFactory interface {
	// AcquireRequest obtains a pooled [Request] instance.
	AcquireRequest() Request
	// ReleaseRequest releases a pooled [Request] instance back to the memory pool.
	ReleaseRequest(req Request)
}

// BaseResponseProvider provides a [BaseResponse] model factory for structured envelope unwrapping.
type BaseResponseProvider interface {
	// BaseResponse constructs or returns a zero-value BaseResponse envelope instance.
	BaseResponse() BaseResponse
}

// QueryEncoder marshals arbitrary structures or maps into [url.Values].
type QueryEncoder func(any) (url.Values, error)

// WebSocketDialer is implemented by clients supporting raw TCP/TLS socket dialing
// for WebSocket upgrades over uTLS or HTTP/2 Extended CONNECT (RFC 8441).
type WebSocketDialer interface {
	// DialTLSForWS opens an encrypted TLS connection for WebSockets.
	DialTLSForWS(ctx context.Context, addr string) (net.Conn, error)
	// DialPlainForWS opens an unencrypted TCP connection for WebSockets.
	DialPlainForWS(ctx context.Context, addr string) (net.Conn, error)
}

// DNSResolver defines the hostname-to-IP lookup resolution contract.
// Implemented by DoH (RFC 8484), DoT (RFC 7858), DoQ (RFC 9250), and static resolvers.
type DNSResolver interface {
	// LookupIPAddr performs DNS A/AAAA resolution for host.
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SocketController provides a low-level OS kernel hook executed after socket creation
// but prior to TCP SYN packet transmission.
// Allows applying socket options such as SO_MARK, TCP_MAXSEG, TCP_QUICKACK, or p0f signatures.
type SocketController interface {
	// Control applies OS kernel socket options to file descriptor fd.
	Control(fd uintptr, network, address string) error
}

// SessionCache extends the uTLS ClientSessionCache contract to support proxy-isolated TLS 1.3 session tickets.
// Ticket storage is partitioned per proxy exit node to prevent cross-proxy tracking via session tickets (RFC 8446 / RFC 9001).
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

// CacheStore defines the persistence interface for HTTP response caching backends (e.g. Memory, Redis).
type CacheStore interface {
	// Get retrieves cached payload by key.
	Get(ctx context.Context, key any) ([]byte, error)
	// Set stores cached payload by key with ttl expiration.
	Set(ctx context.Context, key any, val []byte, ttl time.Duration) error
}

// ChallengeSolver delegates WAF/DDoS challenge page resolution (e.g. Cloudflare JS/Captcha)
// to automated headless or external solver drivers.
type ChallengeSolver interface {
	// Solve resolves a WAF challenge response and retries the request.
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

// ChallengeDetector determines whether an incoming HTTP response represents a WAF/DDoS challenge page.
type ChallengeDetector func(resp *http.Response) (bool, error)

// TrafficInspector captures and records fine-grained request traces, headers, and JA4 signatures
// for real-time diagnostic web dashboard inspection.
type TrafficInspector interface {
	// Capture records execution metrics for an HTTP transaction.
	Capture(req *http.Request, resp *http.Response, err error, traceInfo *telemetry.TraceInfo)
}

// HARTracker records HTTP transaction details into HAR 1.2 JSON format logs.
type HARTracker interface {
	// Record records transaction telemetry into HAR logs.
	Record(req *http.Request, resp *http.Response, startTime time.Time, duration int64)
}

// Logger specifies the structured diagnostic logging interface.
// Fully compatible with slog.Logger, zap.Logger, zerolog, and stdlib log.
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
	// Logger returns the active diagnostic logger.
	Logger() Logger
}

var noopReleaseFunc = func() {}

// AcquireRequest obtains a pooled [Request] instance from doer if supported via [RequestFactory],
// or allocates a standard request wrapper. Returns the request and a release cleanup closure.
// Calling the release closure returns pooled resources back to memory pools safely.
func AcquireRequest(doer any) (Request, func()) {
	if factory, ok := doer.(RequestFactory); ok {
		r := factory.AcquireRequest()
		return r, func() { factory.ReleaseRequest(r) }
	}

	stdReq := NewStdRequest(nil)

	return stdReq, noopReleaseFunc
}
