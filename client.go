// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/h2"
)

const (
	// AlpnH3 is the official ALPN protocol identifier for HTTP/3 over QUIC.
	AlpnH3 = "h3"
	// AlpnH2 is the official ALPN protocol identifier for HTTP/2 over TLS.
	AlpnH2 = "h2"
	// AlpnHTTP is the official ALPN protocol identifier for HTTP/1.1.
	AlpnHTTP = "http/1.1"
)

// HTTPDoer executes an [http.Request] and returns a response.
// [http.Client] satisfies this interface. Pass a [DoerFunc] to adapt
// a plain function.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc adapts a function to the [HTTPDoer] interface.
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do calls f(req).
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Requester sends an HTTP request and returns the response.
// [Client] is the primary implementation. Relative paths are resolved
// against the base URL. Request modifiers are applied before execution.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		mods ...RequestModifier,
	) (*http.Response, error)
}

// BaseResponseProvider optionally provides a [BaseResponse] for
// structured decoding. Implemented by response wrapper types used
// with [option.WithBaseResponse].
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// ProgressFunc is called periodically during response body reads.
// current is the bytes read so far; total is the Content-Length
// value or -1 if unknown.
type ProgressFunc func(current, total int64)

// BaseResponse is implemented by user-defined response wrappers that
// participate in [GetTo] and similar generic request helpers. The
// decoder calls IsSuccess, SetData, and Error to route the result.
type BaseResponse interface {
	// IsSuccess reports whether the response indicates a successful operation.
	IsSuccess() bool
	// Error returns an error representation if IsSuccess returns false.
	Error() error
	// SetData sets the data into the response.
	SetData(data any)
}

// Logger is an interface for logging messages.
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

// ClientOption is a functional option that configures a [Config] and is
// consumed by [NewClient] or [Client.With] to produce a configured client.
// Concrete option implementations are located in the [github.com/lemon4ksan/aoni/option] package.
type ClientOption func(cfg *Config)

// Client is an immutable, concurrency-safe HTTP client built on [HTTPDoer].
// Every With* method returns a new clone, so the original remains usable
// by other goroutines. Use [NewClient] to create the first instance.
type Client struct {
	engine      HTTPDoer
	network     NetworkConfig
	fingerprint FingerprintConfig
	defaults    ClientDefaults

	userAgentRotationCounter uint32
	proxyFailoverCounter     uint32
}

// NewClient creates a [Client] wrapping httpClient. When httpClient
// is nil a default [http.Client] with a 15-second timeout and
// [DefaultRedirectPolicy] (10 hops) is used. The returned client
// has [DefaultUserAgent] set and a transport dialer configured for
// Happy Eyeballs.
func NewClient(doer HTTPDoer, opts ...ClientOption) *Client {
	if doer == nil {
		doer = &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: DefaultRedirectPolicy(10),
		}
	}

	c := &Client{
		engine: doer,
		defaults: ClientDefaults{
			BaseURL:         &url.URL{},
			Headers:         make(http.Header),
			MaxResponseSize: 10 * 1024 * 1024,
			RefererState:    &RefererState{},
			Pipeline: PipelineConfig{
				Decompress: true,
				Validate:   true,
				Challenge:  true,
			},
		},
		network: NetworkConfig{
			HappyEyeballsDelay: 300 * time.Millisecond,
		},
	}

	// Seed a Config from the freshly constructed client, apply all options
	// to the Config snapshot, then write back to c — same three-phase flow
	// as Client.With so that every option stays purely data-oriented.
	cfg := Config{
		Network:     c.network.Clone(),
		Fingerprint: c.fingerprint.Clone(),
		Defaults:    c.defaults.Clone(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	c.network = cfg.Network
	c.fingerprint = cfg.Fingerprint
	c.defaults = cfg.Defaults

	// Apply engine-level overrides (Timeout, RedirectLimit, CookieJar, …).
	applyEngineConfig(c, cfg.Engine)

	// Wire transport dialers for the final network/fingerprint configuration.
	c.applyDialers(c.Transport())

	// Apply H2 framing / configurer if fingerprint settings were provided.
	c.reapplyH2Settings(c.Transport())

	// Default to user agent if not set.
	if c.defaults.Headers.Get("User-Agent") == "" {
		c.defaults.Headers.Set("User-Agent", DefaultUserAgent)
	}

	return c
}

// Engine returns the raw underlying HTTPDoer (typically *http.Client) without any middleware wrappers.
func (c *Client) Engine() HTTPDoer {
	return c.engine
}

// Defaults returns the ClientDefaults configured on c.
func (c *Client) Defaults() ClientDefaults {
	return c.defaults
}

// Network returns the NetworkConfig configured on c.
func (c *Client) Network() NetworkConfig {
	return c.network
}

// Fingerprint returns the FingerprintConfig configured on c.
func (c *Client) Fingerprint() FingerprintConfig {
	return c.fingerprint
}

// Inspector returns the configured [TrafficInspector] if enabled.
func (c *Client) Inspector() TrafficInspector {
	return c.defaults.Inspector
}

// TLSConfig returns the transport's TLS client config.
func (c *Client) TLSConfig() *tls.Config {
	if tr := c.Transport(); tr != nil && tr.TLSClientConfig != nil {
		return tr.TLSClientConfig.Clone()
	}

	return nil
}

// BrowserID returns the configured BrowserID.
func (c *Client) BrowserID() BrowserID {
	if c.fingerprint.BrowserID != BrowserNone {
		return c.fingerprint.BrowserID
	}

	if httpClient, ok := c.engine.(*http.Client); ok {
		if tr, ok := httpClient.Transport.(*http.Transport); ok {
			if tr != nil && tr.DialTLSContext != nil {
				return BrowserChrome
			}
		}
	}

	return BrowserNone
}

// Logger returns the logger used by the client.
// If no logger is set, a no-op logger is returned.
func (c *Client) Logger() Logger {
	if c.defaults.Logger == nil {
		return log.Discard
	}

	return c.defaults.Logger
}

// HTTP returns an HTTPDoer that executes requests through the client's pipeline.
func (c *Client) HTTP() HTTPDoer {
	return DoerFunc(func(req *http.Request) (*http.Response, error) {
		return c.execute(req, c.resolvePipeline(req))
	})
}

// Transport returns the underlying [http.Transport] of the client.
// Returns nil if the [HTTPDoer] is not an [http.Client] or its transport is not an [http.Transport].
func (c *Client) Transport() *http.Transport {
	httpClient, ok := c.engine.(*http.Client)
	if !ok {
		return nil
	}

	if httpClient.Transport == nil {
		httpClient.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	curr := httpClient.Transport
	for {
		switch tr := curr.(type) {
		case *http.Transport:
			return tr
		case *h2.FramedTransport:
			curr = tr.Transport
		case *cookie.Transport:
			curr = tr.Unwrap()
		default:
			return nil
		}
	}
}

// Get performs a GET request through the Client and returns the raw http.Response.
func (c *Client) Get(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return Get(ctx, c, path, mods...)
}

// Post executes a POST request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Post(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Post(ctx, c, path, body, mods...)
}

// Put executes a PUT request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Put(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Put(ctx, c, path, body, mods...)
}

// Patch executes a PATCH request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Patch(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Patch(ctx, c, path, body, mods...)
}

// Delete executes a DELETE request through the Client and returns the raw http.Response.
//
// By default, if the body is a struct or map, it is marshaled to JSON and the request headers
// "Content-Type" and "Accept" are set to "application/json".
//
// To send other body formats (e.g. XML, YAML, or plain text), pre-serialize the payload and
// pass it as an [io.Reader] (e.g. using [strings.NewReader] or [bytes.NewReader]), then override the Content-Type
// header using request modifiers like [WithContentType] (e.g. WithContentType("application/xml")).
func (c *Client) Delete(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return Delete(ctx, c, path, body, mods...)
}

// Request sends an HTTP request and returns the response. path is
// resolved against [option.WithBaseURL] when set; an empty path
// targets the base URL directly. Nil modifiers are ignored.
//
// Decompression (gzip, brotli, zstd) and charset transcoding to
// UTF-8 are applied automatically.
//
// Returns [ErrSSRFBlocked] when SSRF guarding is on and the target
// resolves to a private or loopback address. Returns
// [ErrResponseTooLarge] when a response size limit is configured
// and the body exceeds it.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("aoni: invalid path: %w", err)
	}

	u := c.defaults.BaseURL.ResolveReference(rel)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), http.NoBody) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("aoni: failed to create request: %w", err)
	}

	maps.Copy(req.Header, c.defaults.Headers)

	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "zstd, br, gzip")
	}

	req = c.InitRequestConfig(req)

	generic.ApplyOptions(req, c.defaults.DefaultMods...)
	generic.ApplyOptions(req, mods...)

	if cfg := GetRequestConfig(req.Context()); cfg != nil {
		if cfg.BodyError != nil {
			return nil, fmt.Errorf("aoni: body encoding failed: %w", cfg.BodyError)
		}

		if cfg.QueryError != nil {
			return nil, fmt.Errorf("aoni: query encoding failed: %w", cfg.QueryError)
		}
	}

	resp, err := c.execute(req, c.resolvePipeline(req))
	if err != nil {
		return nil, fmt.Errorf("aoni: request failed: %w", err)
	}

	return resp, nil
}
