// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"

	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
)

// DefaultUserAgent is the default User-Agent string used for HTTP requests.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var (
	bytePool = sync.Pool{
		New: func() any {
			b := make([]byte, 32*1024)
			return &b
		},
	}
	bufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
)

// Unwrapper allows nested decorators to be peeled away to reach the
// underlying [Requester]. [Client] does not implement this interface;
// wrapper types returned by [NewStdClient] or [Chain] do.
type Unwrapper interface {
	Unwrap() Requester
}

// UnwrapClient strips all [Unwrapper] layers from r and returns the
// innermost [Client]. Returns nil if r is not a *Client and no
// Unwrapper chain leads to one.
func UnwrapClient(r Requester) *Client {
	for {
		if client, ok := r.(*Client); ok {
			return client
		}

		u, ok := r.(Unwrapper)
		if !ok {
			break
		}

		r = u.Unwrap()
	}

	return nil
}

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
// with [WithClientBaseResponse].
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
func NewClient(httpClient HTTPDoer, opts ...ClientOption) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: DefaultRedirectPolicy(10),
		}
	}

	c := &Client{
		engine: httpClient,
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

	c.applyDialers()
	generic.ApplyOptions(c, opts...)

	// Default to user agent if not set
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
	if httpClient, ok := c.engine.(*http.Client); ok {
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
		for curr != nil {
			if transport, ok := curr.(*http.Transport); ok {
				return transport
			}

			if ft, ok := curr.(*H2FramedTransport); ok {
				curr = ft.Transport
				continue
			}

			if cj, ok := curr.(*cookieJarTransport); ok {
				curr = cj.next
				continue
			}

			break
		}
	}

	return nil
}

// With returns a clone of c with the specified functional options applied.
func (c *Client) With(opts ...ClientOption) *Client {
	cloned := c.Clone()
	generic.ApplyOptions(cloned, opts...)
	cloned.applyDialers()

	return cloned
}

// Clone returns a deep copy of c. The cloned client shares nothing
// mutable with the original - transport, cookie jar, and config
// structs are all independently copied.
func (c *Client) Clone() *Client {
	cloned := &Client{
		network:     c.network,
		fingerprint: c.fingerprint,
		defaults:    c.defaults,
	}

	if c.network.DynamicHedging != nil {
		dhCopy := *c.network.DynamicHedging
		cloned.network.DynamicHedging = &dhCopy
	}

	if c.network.FragmentConfig != nil {
		fragCopy := *c.network.FragmentConfig
		cloned.network.FragmentConfig = &fragCopy
	}

	if c.network.HostRewrite != nil && c.network.HostRewrite.Rules != nil {
		rulesCopy := make(map[string]string, len(c.network.HostRewrite.Rules))
		maps.Copy(rulesCopy, c.network.HostRewrite.Rules)
		cloned.network.HostRewrite = &HostRewriteConfig{Rules: rulesCopy}
	}

	if c.fingerprint.TLSClientHelloID != nil {
		idCopy := *c.fingerprint.TLSClientHelloID
		cloned.fingerprint.TLSClientHelloID = &idCopy
	}

	if c.fingerprint.HeaderOrder != nil {
		orderCopy := make([]string, len(c.fingerprint.HeaderOrder))
		copy(orderCopy, c.fingerprint.HeaderOrder)
		cloned.fingerprint.HeaderOrder = orderCopy
	}

	if c.fingerprint.P0fSignature != nil {
		sigCopy := *c.fingerprint.P0fSignature
		if len(sigCopy.Options) > 0 {
			optsCopy := make([]string, len(sigCopy.Options))
			copy(optsCopy, sigCopy.Options)
			sigCopy.Options = optsCopy
		}

		if len(sigCopy.Quirks) > 0 {
			qCopy := make([]string, len(sigCopy.Quirks))
			copy(qCopy, sigCopy.Quirks)
			sigCopy.Quirks = qCopy
		}

		cloned.fingerprint.P0fSignature = &sigCopy
	}

	if c.fingerprint.H2Settings != nil {
		h2Copy := *c.fingerprint.H2Settings
		cloned.fingerprint.H2Settings = &h2Copy
	}

	if c.fingerprint.H3Settings != nil {
		h3Copy := *c.fingerprint.H3Settings
		cloned.fingerprint.H3Settings = &h3Copy
	}

	if c.fingerprint.PacketPadding != nil {
		padCopy := *c.fingerprint.PacketPadding
		cloned.fingerprint.PacketPadding = &padCopy
	}

	cloned.defaults.Headers = c.defaults.Headers.Clone()

	beforeCopy := make([]func(req *http.Request), len(c.defaults.BeforeRequest))
	copy(beforeCopy, c.defaults.BeforeRequest)
	cloned.defaults.BeforeRequest = beforeCopy

	afterCopy := make([]func(resp *http.Response, err error), len(c.defaults.AfterResponse))
	copy(afterCopy, c.defaults.AfterResponse)
	cloned.defaults.AfterResponse = afterCopy

	if c.defaults.DefaultMods != nil {
		modsCopy := make([]RequestModifier, len(c.defaults.DefaultMods))
		copy(modsCopy, c.defaults.DefaultMods)
		cloned.defaults.DefaultMods = modsCopy
	}

	// Clone PipelineConfig
	cloned.defaults.Pipeline = c.defaults.Pipeline
	if c.defaults.Pipeline.DPIJitter != nil {
		dj := *c.defaults.Pipeline.DPIJitter
		cloned.defaults.Pipeline.DPIJitter = &dj
	}

	if c.defaults.Pipeline.ProxyFailover != nil {
		pf := *c.defaults.Pipeline.ProxyFailover
		proxiesCopy := make([]string, len(pf.Proxies))
		copy(proxiesCopy, pf.Proxies)
		pf.Proxies = proxiesCopy
		cloned.defaults.Pipeline.ProxyFailover = &pf
	}

	if c.defaults.Pipeline.Hedging != nil {
		h := *c.defaults.Pipeline.Hedging
		if h.DynamicHedging != nil {
			dhCopy := *h.DynamicHedging
			h.DynamicHedging = &dhCopy
		}

		cloned.defaults.Pipeline.Hedging = &h
	}

	if c.defaults.Pipeline.Cache != nil {
		cc := *c.defaults.Pipeline.Cache
		cloned.defaults.Pipeline.Cache = &cc
	}

	if c.defaults.Pipeline.HAR != nil {
		har := *c.defaults.Pipeline.HAR
		cloned.defaults.Pipeline.HAR = &har
	}

	if c.defaults.Pipeline.Redact != nil {
		r := *c.defaults.Pipeline.Redact
		if r.Headers != nil {
			headersCopy := make(map[string]bool, len(r.Headers))
			for k, v := range r.Headers {
				headersCopy[k] = v
			}

			r.Headers = headersCopy
		}

		cloned.defaults.Pipeline.Redact = &r
	}

	// Clone UARotationProfiles
	if c.defaults.UARotationProfiles != nil {
		profilesCopy := make([]BrowserProfile, len(c.defaults.UARotationProfiles))
		for i, prof := range c.defaults.UARotationProfiles {
			hintsCopy := make(map[string]string, len(prof.ClientHints))
			for k, v := range prof.ClientHints {
				hintsCopy[k] = v
			}

			profilesCopy[i] = BrowserProfile{
				UserAgent:   prof.UserAgent,
				ClientHints: hintsCopy,
			}
		}

		cloned.defaults.UARotationProfiles = profilesCopy
	}

	// Clone http.Client and its transport to avoid race conditions.
	// If the transport is wrapped in cookieJarTransport, unwrap, clone the
	// base transport, and re-wrap to preserve the cookie jar binding.
	cloned.engine = c.engine
	if httpClient, ok := cloned.engine.(*http.Client); ok {
		clonedHTTP := *httpClient
		baseTransport := clonedHTTP.Transport

		var wrappedJar *ProxyIsolatedCookieJar

		if cjTrans, ok := baseTransport.(*cookieJarTransport); ok {
			wrappedJar = cjTrans.cookieJar
			baseTransport = cjTrans.next
		}

		if transport, ok := baseTransport.(*http.Transport); ok && transport != nil {
			baseTransport = transport.Clone()
		}

		if wrappedJar != nil {
			clonedHTTP.Transport = &cookieJarTransport{
				next:      baseTransport,
				cookieJar: wrappedJar,
			}
		} else {
			clonedHTTP.Transport = baseTransport
		}

		cloned.engine = &clonedHTTP
	}

	cloned.applyDialers()

	return cloned
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

// InitRequestConfig initializes the request configuration for the given request.
func (c *Client) InitRequestConfig(req *http.Request) *http.Request {
	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = &RequestConfig{
			Metadata: make(map[string]any),
		}
		ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
		req = req.WithContext(ctx)
	}

	if !cfg.SSRFGuard {
		cfg.SSRFGuard = c.network.SSRFGuard
	}

	if cfg.HappyEyeballsDelay == 0 {
		cfg.HappyEyeballsDelay = c.network.HappyEyeballsDelay
	}

	if !cfg.ProxyDNS {
		cfg.ProxyDNS = c.network.ProxyDNS
	}

	if cfg.ProxyAddr == nil {
		cfg.ProxyAddr = c.network.ProxyAddr
	}

	if cfg.P0fSignature == nil {
		cfg.P0fSignature = c.fingerprint.P0fSignature
	}

	if cfg.SessionCache == nil {
		cfg.SessionCache = c.fingerprint.SessionCache
	}

	if cfg.PacketPadding == nil {
		cfg.PacketPadding = c.fingerprint.PacketPadding
	}

	if cfg.SocketController == nil {
		cfg.SocketController = c.network.SocketController
	}

	if cfg.ClientHelloSpecProvider == nil {
		cfg.ClientHelloSpecProvider = c.fingerprint.TLSClientHelloSpecProvider
	}

	if cfg.JA4Callback == nil {
		cfg.JA4Callback = c.fingerprint.JA4Callback
	}

	if cfg.MultiReadThreshold == 0 {
		cfg.MultiReadThreshold = c.defaults.MultiReadThreshold
	}

	if !cfg.MultiReadDisableDisk {
		cfg.MultiReadDisableDisk = c.defaults.MultiReadDisableDisk
	}

	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	if cfg.QueryEncoder == nil {
		cfg.QueryEncoder = c.defaults.QueryEncoder
	}

	if len(c.fingerprint.CertificatePins) > 0 {
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		for domain, hashes := range c.fingerprint.CertificatePins {
			for _, h := range hashes {
				found := false
				for _, existing := range cfg.CertificatePins[domain] {
					if existing == h {
						found = true
						break
					}
				}

				if !found {
					cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], h)
				}
			}
		}
	}

	return req
}

// Request sends an HTTP request and returns the response. path is
// resolved against [WithClientBaseURL] when set; an empty path
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

	resp, reqErr := c.execute(req, c.resolvePipeline(req))
	if reqErr != nil {
		return nil, fmt.Errorf("aoni: request failed: %w", reqErr)
	}

	return resp, nil
}

// DialTLSForWS dials a TLS connection, routing through the transport's
// DialTLSContext when available.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	if tr := c.Transport(); tr != nil && tr.DialTLSContext != nil {
		network := "tcp"
		return tr.DialTLSContext(ctx, network, addr)
	}

	browser := c.BrowserID()
	if browser != BrowserNone || c.fingerprint.TLSClientHelloID != nil {
		var proxyURL *url.URL
		if c.network.TransportProxy != nil {
			proxyURL, _ = c.network.TransportProxy(&http.Request{URL: &url.URL{Host: addr}})
		}

		return dialTLSWithUTLS(
			ctx,
			"tcp",
			addr,
			browser,
			c.fingerprint.TLSClientHelloID,
			c.network.SourceRotator,
			c.network.DNSResolver,
			c.fingerprint.JA4Callback,
			c.TLSConfig(),
			proxyURL,
		)
	}

	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		return tr.DialContext(ctx, "tcp", addr)
	}

	return dialStandardTLS(ctx, addr)
}

// DialPlainForWS dials a plain TCP connection, routing through the transport's
// DialContext when available.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	var (
		conn net.Conn
		err  error
	)

	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		conn, err = tr.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = cleanDialContext(
			ctx,
			"tcp",
			addr,
			c.network.HappyEyeballsDelay,
			c.network.SSRFGuard,
			c.network.SourceRotator,
			c.network.DNSResolver,
		)
	}

	if err != nil {
		return nil, err
	}

	var fCfg *FragmentConfig
	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		fCfg = cfg.Fragment
	} else if val := ctx.Value(fragmentCtxKey{}); val != nil {
		if fc, ok := val.(FragmentConfig); ok {
			fCfg = &fc
		}
	}

	if fCfg != nil {
		conn = wrapWithFragmentation(conn, *fCfg)
	}

	return conn, nil
}

// dialStandardTLS dials using Go's standard net.Dialer (no fingerprint, no proxy).
func dialStandardTLS(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return dialer.DialContext(ctx, "tcp", addr)
}

func (c *Client) resolvePipeline(req *http.Request) PipelineConfig {
	if reqPipe, ok := GetPipeline(req.Context()); ok {
		return reqPipe
	}

	pipe := c.defaults.Pipeline

	if !pipe.RotateUA && len(c.defaults.UARotationProfiles) > 0 {
		pipe.RotateUA = true
	}

	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.defaults.MaxResponseSize
	}

	if !pipe.Inspect && c.defaults.Inspector != nil {
		pipe.Inspect = true
	}

	if pipe.Hedging == nil && (c.network.HedgingDelay > 0 || c.network.DynamicHedging != nil) {
		pipe.Hedging = &HedgingConfig{
			DefaultDelay:   c.network.HedgingDelay,
			DynamicHedging: c.network.DynamicHedging,
		}
	}

	return pipe
}

func (c *Client) execute(req *http.Request, pipe PipelineConfig) (*http.Response, error) {
	startTime := time.Now()

	ctx := req.Context()

	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.TimeoutOverride > 0 {
			var cancel context.CancelFunc

			ctx, cancel = context.WithTimeout(ctx, cfg.TimeoutOverride) //nolint:gosec
			cfg.RequestTimeoutCancel = cancel
		}

		if cfg.SessionCache != nil && cfg.ProxyAddr != nil {
			cfg.SessionCache.SetProxyKey(cfg.ProxyAddr.String())
		}

		if cfg.ProxyAddr != nil {
			ctx = context.WithValue(ctx, proxyCtxKey{}, cfg.ProxyAddr.String())
		}
	}

	req = req.WithContext(ctx)

	for _, hook := range c.defaults.BeforeRequest {
		hook(req)
	}

	if c.fingerprint.PacketPadding != nil {
		c.applyPacketPadding(req)
	}

	if c.defaults.RefererAutomaton {
		c.applyRefererHeader(req)
	}

	if pipe.RotateUA {
		c.rotateUserAgentAndHints(req)
	}

	if pipe.DPIJitter != nil {
		c.applyDPIJitter(req, pipe.DPIJitter)
	}

	if pipe.Redact != nil {
		req = c.redactSensitiveData(req, pipe.Redact)
	}

	if pipe.Cache != nil && req.Method == http.MethodGet {
		if cachedResp, err := c.tryGetFromCache(req, pipe.Cache); err == nil {
			return cachedResp, nil
		}
	}

	var (
		traceInfo *TraceInfo
		traceEnd  func(*http.Response)
	)

	if cfg != nil && cfg.TraceInfo != nil {
		traceInfo = cfg.TraceInfo
	} else if pipe.Inspect && c.defaults.Inspector != nil {
		traceInfo = &TraceInfo{}

		store := &ja4ReportStore{target: traceInfo}
		if cfg != nil {
			cfg.JA4ReportStore = store
		}

		traceInfo.JA4 = &ja4.Report{JA4H: computeJA4HFromRequest(req)}
	}

	if traceInfo != nil {
		trace := &httptrace.ClientTrace{
			DNSStart:          func(_ httptrace.DNSStartInfo) { traceInfo.dnsStart = time.Now() },
			DNSDone:           func(_ httptrace.DNSDoneInfo) { traceInfo.DNSLookup = time.Since(traceInfo.dnsStart) },
			ConnectStart:      func(_, _ string) { traceInfo.connectStart = time.Now() },
			ConnectDone:       func(_, _ string, _ error) { traceInfo.TCPConn = time.Since(traceInfo.connectStart) },
			TLSHandshakeStart: func() { traceInfo.tlsStart = time.Now() },
			TLSHandshakeDone:  func(_ tls.ConnectionState, _ error) { traceInfo.TLSHandshake = time.Since(traceInfo.tlsStart) },
			GotConn: func(info httptrace.GotConnInfo) {
				traceInfo.gotConn = time.Now()
				if info.Conn != nil && info.Conn.RemoteAddr() != nil {
					traceInfo.RemoteAddr = info.Conn.RemoteAddr().String()
				}
			},
			GotFirstResponseByte: func() { traceInfo.ServerProcessing = time.Since(traceInfo.gotConn) },
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		traceEnd = traceInfo.Start()
	}

	var (
		resp *http.Response
		err  error
	)

	switch {
	case pipe.ProxyFailover != nil:
		resp, err = c.executeWithProxyFailover(req, pipe.ProxyFailover, pipe.Hedging)
	case pipe.Hedging != nil:
		resp, err = c.executeWithHedging(req, pipe.Hedging)
	default:
		resp, err = c.engine.Do(req)
	}

	duration := time.Since(startTime).Milliseconds()

	for _, hook := range c.defaults.AfterResponse {
		hook(resp, err)
	}

	if err != nil {
		return nil, err
	}

	if cfg != nil && cfg.JA4ReportStore != nil && cfg.JA4ReportStore.report != nil {
		store := cfg.JA4ReportStore
		store.target.JA4.JA4 = store.report.JA4
		store.target.JA4.Protocol = store.report.Protocol
		store.target.JA4.Version = store.report.Version
		store.target.JA4.SNI = store.report.SNI
		store.target.JA4.CipherCount = store.report.CipherCount
		store.target.JA4.ExtCount = store.report.ExtCount
		store.target.JA4.ALPN = store.report.ALPN
	}

	if traceEnd != nil {
		traceEnd(resp)
	}

	if pipe.Inspect && c.defaults.Inspector != nil {
		c.captureTraffic(req, resp, err, traceInfo)
	}

	if pipe.HAR != nil {
		c.writeHARLog(req, resp, pipe.HAR, startTime, duration)
	}

	if pipe.SizeLimit > 0 {
		if limitErr := c.limitResponseSize(resp, pipe.SizeLimit); limitErr != nil {
			return nil, limitErr
		}
	}

	if pipe.Decompress {
		resp = c.handleDecompressionAndTranscoding(req, resp)
	}

	if pipe.Challenge {
		resp, err = c.handleWAFChallenge(req, resp)
		if err != nil {
			return nil, err
		}
	}

	if pipe.Validate {
		if valErr := c.validateResponse(resp); valErr != nil {
			return nil, valErr
		}
	}

	if c.defaults.RefererAutomaton && c.defaults.RefererState != nil && req != nil && req.URL != nil {
		c.defaults.RefererState.mu.Lock()
		c.defaults.RefererState.lastURL = req.URL.String()
		c.defaults.RefererState.mu.Unlock()
	}

	if resp != nil && resp.Body != nil {
		if bufErr := c.applyMultiReadBuffering(req, resp, cfg); bufErr != nil {
			return nil, bufErr
		}

		resp.Body = newResponseBodyReadCloser(resp.Body)
	}

	if pipe.Cache != nil && req.Method == http.MethodGet {
		c.saveToCache(req, resp, pipe.Cache)
	}

	return resp, nil
}

func (c *Client) rotateUserAgentAndHints(req *http.Request) {
	profiles := c.defaults.UARotationProfiles
	if len(profiles) == 0 {
		profiles = DefaultBrowserProfiles
	}

	idx := atomic.AddUint32(&c.userAgentRotationCounter, 1) - 1
	prof := profiles[idx%uint32(len(profiles))] //nolint:gosec

	req.Header.Set("User-Agent", prof.UserAgent)

	for k, v := range prof.ClientHints {
		req.Header.Set(k, v)
	}
}

func (c *Client) applyDPIJitter(req *http.Request, cfg *DPIJitterConfig) {
	var delay time.Duration
	if cfg.MinDelay > 0 && cfg.MaxDelay >= cfg.MinDelay {
		delta := cfg.MaxDelay - cfg.MinDelay
		if delta > 0 {
			r := time.Duration(time.Now().UnixNano() % int64(delta))
			delay = cfg.MinDelay + r
		} else {
			delay = cfg.MinDelay
		}
	}

	if delay > 0 {
		if req.Body != nil && req.Body != http.NoBody {
			req.Body = &jitterReader{
				ReadCloser: req.Body,
				delay:      delay,
			}
		} else {
			time.Sleep(delay)
		}
	}
}

func (c *Client) tryGetFromCache(req *http.Request, cfg *CacheConfig) (*http.Response, error) {
	if req.Method != http.MethodGet || cfg == nil || cfg.Store == nil {
		return nil, errors.New("aoni cache: bypass")
	}

	cc := req.Header.Get("Cache-Control")
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return nil, errors.New("aoni cache: bypass via request header")
	}

	cacheKey := req.Method + ":" + req.URL.String()

	cachedData, err := cfg.Store.Get(req.Context(), cacheKey)
	if err != nil {
		return nil, err
	}

	var cached cachedResponse
	if decodeErr := json.Unmarshal(cachedData, &cached); decodeErr != nil {
		return nil, decodeErr
	}

	bodyBytes, _ := base64.StdEncoding.DecodeString(cached.BodyBase64)
	resp := &http.Response{
		StatusCode:    cached.StatusCode,
		Header:        cached.Header,
		Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
		ContentLength: int64(len(bodyBytes)),
		Request:       req,
	}

	return resp, nil
}

func (c *Client) saveToCache(req *http.Request, resp *http.Response, cfg *CacheConfig) {
	if req.Method != http.MethodGet || resp == nil || resp.StatusCode != http.StatusOK || cfg == nil ||
		cfg.Store == nil {
		return
	}

	respCC := resp.Header.Get("Cache-Control")
	if strings.Contains(respCC, "no-store") || strings.Contains(respCC, "private") {
		return
	}

	var bodyBuf bytes.Buffer

	tee := io.TeeReader(resp.Body, &bodyBuf)

	bodyBytes, readErr := io.ReadAll(tee)
	if readErr != nil {
		return
	}

	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	cached := cachedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		BodyBase64: base64.StdEncoding.EncodeToString(bodyBytes),
	}

	if cachedData, marshalErr := json.Marshal(cached); marshalErr == nil {
		ttl := cfg.DefaultTTL
		if reqCfg := GetRequestConfig(req.Context()); reqCfg != nil && reqCfg.CacheTTL > 0 {
			ttl = reqCfg.CacheTTL
		}

		_ = cfg.Store.Set(req.Context(), req.Method+":"+req.URL.String(), cachedData, ttl)
	}
}

func (c *Client) redactSensitiveData(req *http.Request, redact *RedactConfig) *http.Request {
	headersMap := make(map[string]bool)
	for _, h := range redact.HeadersToRedact {
		headersMap[strings.ToLower(h)] = true
	}

	if len(headersMap) == 0 {
		headersMap["authorization"] = true
		headersMap["cookie"] = true
		headersMap["set-cookie"] = true
	}

	ctx := context.WithValue(req.Context(), RedactConfigCtxKey{}, &RedactConfig{Headers: headersMap})

	return req.WithContext(ctx)
}

func (c *Client) writeHARLog(
	req *http.Request,
	resp *http.Response,
	har *HARConfig,
	startTime time.Time,
	duration int64,
) {
	if har == nil || har.Generator == nil || resp == nil {
		return
	}

	var reqHeaders []HARHeaderField
	for k, v := range req.Header {
		for _, val := range v {
			reqHeaders = append(reqHeaders, HARHeaderField{Name: k, Value: val})
		}
	}

	var reqBodySize int64
	if req.Body != nil && req.Body != http.NoBody {
		if req.ContentLength > 0 {
			reqBodySize = req.ContentLength
		}
	}

	var respHeaders []HARHeaderField
	for k, v := range resp.Header {
		for _, val := range v {
			respHeaders = append(respHeaders, HARHeaderField{Name: k, Value: val})
		}
	}

	var bodyBytes []byte
	if resp.Body != nil {
		bodyBytes, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	entry := HAREntry{
		StartedDateTime: startTime.UTC().Format(time.RFC3339Nano),
		Time:            duration,
		Request: HARRequest{
			Method:      req.Method,
			URL:         req.URL.String(),
			HTTPVersion: req.Proto,
			Headers:     reqHeaders,
			Cookies:     []any{},
			QueryString: []any{},
			HeadersSize: -1,
			BodySize:    reqBodySize,
		},
		Response: HARResponse{
			Status:      resp.StatusCode,
			StatusText:  resp.Status,
			HTTPVersion: resp.Proto,
			Headers:     respHeaders,
			Cookies:     []any{},
			Content: HARContent{
				Size:     int64(len(bodyBytes)),
				MimeType: resp.Header.Get("Content-Type"),
				Text:     string(bodyBytes),
			},
			RedirectURL: resp.Header.Get("Location"),
			HeadersSize: -1,
			BodySize:    int64(len(bodyBytes)),
		},
		Cache: struct{}{},
		Timings: HARTimings{
			Send:    0,
			Wait:    duration,
			Receive: 0,
		},
	}

	har.Generator.AddEntry(entry)
}

func (c *Client) captureTraffic(req *http.Request, resp *http.Response, err error, traceInfo *TraceInfo) {
	if c.defaults.Inspector != nil {
		c.defaults.Inspector.Capture(req, resp, err, traceInfo)
	}
}

func (c *Client) limitResponseSize(resp *http.Response, maxSize int64) error {
	if resp == nil || resp.Body == nil || maxSize <= 0 {
		return nil
	}

	if resp.ContentLength > maxSize {
		_ = resp.Body.Close()
		return fmt.Errorf("aoni: response too large: %w", ErrResponseTooLarge)
	}

	resp.Body = &limitCheckingReadCloser{
		ReadCloser: resp.Body,
		limit:      maxSize,
	}

	return nil
}

func (c *Client) validateResponse(resp *http.Response) error {
	if resp == nil || resp.Request == nil {
		return nil
	}

	var clientErr error
	if c.defaults.ResponseValidator != nil {
		clientErr = c.defaults.ResponseValidator(resp)
	}

	fn := GetResponseValidator(resp.Request.Context()) //nolint:bodyclose
	if fn != nil {
		requestErr := fn(resp)
		// Per-request validation was configured and executed.
		// Its result overrides the client-level validator result.
		if requestErr != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			return requestErr
		}

		// Request-level validator succeeded (returned nil).
		// This overrides any client-level validation failure.
		return nil
	}

	// If no request-level validator was configured, return the client-level validation error if any.
	if clientErr != nil {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}

		return clientErr
	}

	return nil
}

func (c *Client) executeWithProxyFailover(
	req *http.Request,
	failover *ProxyFailoverConfig,
	hedging *HedgingConfig,
) (*http.Response, error) {
	var parsed []*url.URL
	for _, p := range failover.Proxies {
		if u, err := url.Parse(p); err == nil {
			parsed = append(parsed, u)
		}
	}

	if len(parsed) == 0 {
		if hedging != nil {
			return c.executeWithHedging(req, hedging)
		}

		return c.engine.Do(req)
	}

	var (
		lastErr error
		resp    *http.Response
	)

	for i := 0; i <= failover.RetryLimit; i++ {
		var idx uint32
		if lastErr != nil {
			idx = atomic.AddUint32(&c.proxyFailoverCounter, 1)
		} else {
			idx = atomic.LoadUint32(&c.proxyFailoverCounter)
		}

		proxy := parsed[idx%uint32(len(parsed))] //nolint:gosec

		newReq := req

		cfg := GetRequestConfig(req.Context())
		if cfg != nil {
			cfg.ProxyAddr = proxy
			ctx := context.WithValue(req.Context(), proxyCtxKey{}, proxy.String())
			newReq = req.WithContext(ctx)
		}

		if req.Body != nil && req.Body != http.NoBody && req.GetBody != nil {
			body, getBodyErr := req.GetBody()
			if getBodyErr == nil {
				newReq.Body = body
			}
		}

		if hedging != nil {
			resp, lastErr = c.executeWithHedging(newReq, hedging)
		} else {
			resp, lastErr = c.engine.Do(newReq)
		}

		if lastErr == nil && resp != nil {
			if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
				return resp, nil
			}

			lastErr = fmt.Errorf("aoni: proxy returned status %d", resp.StatusCode)
			_ = resp.Body.Close()
		}
	}

	return nil, fmt.Errorf("aoni proxy failover: exhausted %d retries, last error: %w", failover.RetryLimit, lastErr)
}

func (c *Client) applyPacketPadding(req *http.Request) {
	if padding := GeneratePadding(*c.fingerprint.PacketPadding); len(padding) > 0 {
		headerName := PaddingHeaderName(*c.fingerprint.PacketPadding)
		req.Header.Set(headerName, hex.EncodeToString(padding))
	}
}

func (c *Client) applyRefererHeader(req *http.Request) {
	if req.Header.Get("Referer") == "" {
		state := c.defaults.RefererState
		state.mu.Lock()
		lastURL := state.lastURL
		state.mu.Unlock()

		if lastURL != "" {
			req.Header.Set("Referer", lastURL)
		}
	}
}

func (c *Client) handleWAFChallenge(req *http.Request, resp *http.Response) (*http.Response, error) {
	if c.defaults.ChallengeSolver == nil {
		return resp, nil
	}

	if resp != nil && resp.Body != nil {
		// Read up to 100 KB explicitly to analyze the body for WAF signatures
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
		if err != nil {
			return resp, nil //nolint:nilerr
		}

		buffered := &ExplicitBufferedBody{
			Prefix: bodyBytes,
			Stream: resp.Body,
		}
		resp.Body = buffered

		detector := generic.CoalesceNil(c.defaults.ChallengeDetector, DefaultChallengeDetector)

		isChallenge, challengeErr := detector(resp)
		if !isChallenge {
			buffered.Rewind()
			return resp, nil
		}

		_ = resp.Body.Close()

		newResp, solveErr := c.defaults.ChallengeSolver.Solve(req.Context(), challengeErr, req)
		if solveErr != nil {
			return nil, solveErr
		}

		return newResp, nil
	}

	return resp, nil
}

func (c *Client) applyMultiReadBuffering(req *http.Request, resp *http.Response, cfg *RequestConfig) error {
	threshold := c.defaults.MultiReadThreshold

	disableDisk := c.defaults.MultiReadDisableDisk
	if cfg != nil {
		threshold = cfg.MultiReadThreshold
		disableDisk = cfg.MultiReadDisableDisk
	}

	if threshold > 0 && resp.Body != nil {
		mBody, err := newMultiReadBody(resp.Body, threshold, disableDisk)
		if err != nil {
			_ = resp.Body.Close()
			return err
		}

		resp.Body = mBody
	}

	return nil
}

func (c *Client) handleDecompressionAndTranscoding(req *http.Request, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	cfg := GetRequestConfig(req.Context())
	if cfg != nil && cfg.DownloadProgress != nil {
		resp.Body = &progressReader{
			reader:     resp.Body,
			total:      resp.ContentLength,
			onProgress: cfg.DownloadProgress,
		}
	}

	switch resp.Header.Get("Content-Encoding") {
	case "br":
		resp.Body = &decompressReadCloser{
			Reader: brotli.NewReader(resp.Body),
			closer: resp.Body,
		}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1

	case "zstd":
		if zstdDec, err := zstd.NewReader(resp.Body); err == nil {
			resp.Body = &decompressReadCloser{
				Reader: zstdDec,
				closer: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
		} else {
			resp.Header.Del("Content-Encoding")
		}

	case "gzip":
		if gzReader, err := gzip.NewReader(resp.Body); err == nil {
			resp.Body = &decompressReadCloser{
				Reader: gzReader,
				closer: resp.Body,
			}
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
		} else {
			resp.Header.Del("Content-Encoding")
		}
	}

	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		if _, params, err := mime.ParseMediaType(contentType); err == nil {
			if charset := params["charset"]; charset != "" {
				charset = strings.ToLower(charset)
				if charset != "utf-8" && charset != "utf8" {
					if enc, err := htmlindex.Get(charset); err == nil {
						resp.Body = struct {
							io.Reader
							io.Closer
						}{
							Reader: transform.NewReader(resp.Body, enc.NewDecoder()),
							Closer: resp.Body,
						}
					}
				}
			}
		}
	}

	return resp
}

func (c *Client) executeWithHedging(req *http.Request, pipeHedging *HedgingConfig) (*http.Response, error) {
	requestStart := time.Now()

	var delay time.Duration

	cfg := GetRequestConfig(req.Context())
	switch {
	case cfg != nil && cfg.HedgingDelayOverride != nil:
		delay = *cfg.HedgingDelayOverride
	case pipeHedging != nil && pipeHedging.DynamicHedging != nil:
		delay = pipeHedging.DynamicHedging.ComputeDelay()
	case pipeHedging != nil:
		delay = pipeHedging.DefaultDelay
	default:
		delay = c.network.HedgingDelay
	}

	var (
		resp *http.Response
		err  error
	)

	if delay > 0 {
		resp, err = c.dispatchHedgingAttempts(req, delay)
	} else {
		resp, err = c.engine.Do(req)
	}

	var tracker *RTTTracker
	if pipeHedging != nil && pipeHedging.DynamicHedging != nil {
		tracker = pipeHedging.DynamicHedging.Tracker
	} else if c.network.DynamicHedging != nil {
		tracker = c.network.DynamicHedging.Tracker
	}

	if tracker != nil && err == nil {
		rtt := time.Since(requestStart)
		tracker.Record(rtt)
	}

	return resp, err
}

func (c *Client) dispatchHedgingAttempts(req *http.Request, delay time.Duration) (*http.Response, error) {
	type result struct {
		resp *http.Response
		err  error
	}

	resultsCh := make(chan result, 2)
	ctx := req.Context()
	ctx1, cancel1 := context.WithCancel(ctx)
	ctx2, cancel2 := context.WithCancel(ctx)

	var (
		cleaned bool
		mu      sync.Mutex
	)

	cleanup := func(winner int) {
		mu.Lock()
		defer mu.Unlock()

		if cleaned {
			return
		}

		cleaned = true

		switch winner {
		case 1:
			cancel2()
		case 2:
			cancel1()
		default:
			cancel1()
			cancel2()
		}
	}
	defer func() { cleanup(0) }()

	cloneReq := func(orig *http.Request, reqCtx context.Context) (*http.Request, error) {
		cloned := orig.Clone(reqCtx)
		if orig.Body != nil && orig.Body != http.NoBody {
			if orig.GetBody != nil {
				body, err := orig.GetBody()
				if err != nil {
					return nil, err
				}

				cloned.Body = body
			} else {
				return nil, errors.New("aoni: request body cannot be duplicated for hedging")
			}
		}

		return cloned, nil
	}

	req1, err := cloneReq(req, ctx1)
	if err != nil {
		return nil, err
	}

	go func() {
		resp, err := c.engine.Do(req1) //nolint:bodyclose
		resultsCh <- result{resp: resp, err: err}
	}()

	timer := time.NewTimer(delay)
	defer timer.Stop()

	var (
		req2Started bool
		firstErr    error
	)

	activeCount := 1

	for activeCount > 0 {
		select {
		case res := <-resultsCh:
			activeCount--

			if res.err == nil {
				winner := 1

				cancelWinner := cancel1
				if res.resp.Request != nil && res.resp.Request.Context() == ctx2 {
					winner = 2
					cancelWinner = cancel2
				}

				cleanup(winner)

				res.resp.Body = &contextCancelingReadCloser{
					ReadCloser: res.resp.Body,
					cancel:     cancelWinner,
				}

				return res.resp, nil
			}

			if firstErr == nil {
				firstErr = res.err
			}

			if activeCount == 0 && !req2Started {
				timer.Stop()

				select {
				case <-timer.C:
				default:
				}

				req2Started = true

				req2, err := cloneReq(req, ctx2)
				if err != nil {
					return nil, err
				}

				activeCount++

				go func() {
					resp, err := c.engine.Do(req2) //nolint:bodyclose
					resultsCh <- result{resp: resp, err: err}
				}()
			}

		case <-timer.C:
			if !req2Started {
				req2Started = true

				req2, err := cloneReq(req, ctx2)
				if err != nil {
					break
				}

				activeCount++

				go func() {
					resp, err := c.engine.Do(req2) //nolint:bodyclose
					resultsCh <- result{resp: resp, err: err}
				}()
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, firstErr
}

// CloseIdleConnections closes any idle keep-alive connections maintained by the client.
// This only works if the underlying [HTTPDoer] is an [http.Client].
func (c *Client) CloseIdleConnections() {
	if httpClient, ok := c.engine.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}
}

func (c *Client) applyDialers() {
	if transport := c.Transport(); transport != nil {
		if c.fingerprint.H2Configurer != nil {
			t2, err := http2.ConfigureTransports(transport)
			if err == nil && t2 != nil {
				t2.TLSClientConfig = transport.TLSClientConfig
				_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
			}
		}

		// Use determineProxy so that per-request → client-level → env priority
		// is respected consistently, including the http.ProxyFromEnvironment fallback.
		transport.Proxy = c.determineProxy

		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// SocketController is retrieved from RequestConfig inside makeDialerControl.
			if err := ApplyTCPDelay(ctx); err != nil {
				return nil, err
			}

			return cleanDialContext(
				ctx,
				network,
				addr,
				c.network.HappyEyeballsDelay,
				c.network.SSRFGuard,
				c.network.SourceRotator,
				c.network.DNSResolver,
			)
		}

		// Wire DialTLSContext only when uTLS is NOT configured (WithTLSFingerprint
		// sets its own DialTLSContext). This gives plain http.Transport connections
		// the same WithInsecureSkipVerify + WithTCPDelay support that uTLS enjoys.
		if transport.DialTLSContext == nil {
			transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				baseTLSCfg := transport.TLSClientConfig
				// Honour WithTCPDelay before opening the TCP connection.
				if err := ApplyTCPDelay(ctx); err != nil {
					return nil, err
				}

				host, _, _ := net.SplitHostPort(addr)
				if host == "" {
					host = addr
				}

				// Dial the raw TCP connection.
				rawConn, err := cleanDialContext(
					ctx,
					network,
					addr,
					c.network.HappyEyeballsDelay,
					c.network.SSRFGuard,
					c.network.SourceRotator,
					c.network.DNSResolver,
				)
				if err != nil {
					return nil, err
				}

				// Build the effective TLS config, applying any per-request overrides.
				effectiveCfg := TLSConfigWithOverride(ctx, baseTLSCfg)
				if effectiveCfg == nil {
					effectiveCfg = &tls.Config{} //nolint:gosec
				}

				// Ensure SNI is set even when config came from the user without it.
				if effectiveCfg.ServerName == "" {
					cloned := effectiveCfg.Clone()
					cloned.ServerName = host
					effectiveCfg = cloned
				}

				// Inject certificate pin verification if configured.
				if cfg := GetRequestConfig(ctx); cfg != nil && len(cfg.CertificatePins) > 0 {
					cloned := effectiveCfg.Clone()
					userVerify := cloned.VerifyPeerCertificate
					cloned.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error { //nolint:gosec
						if err := verifyCertificatePins(host, cfg.CertificatePins, rawCerts); err != nil {
							return err
						}

						if userVerify != nil {
							return userVerify(rawCerts, verifiedChains)
						}

						return nil
					}
					effectiveCfg = cloned
				}

				tlsConn := tls.Client(rawConn, effectiveCfg)
				if err := tlsConn.HandshakeContext(ctx); err != nil {
					_ = rawConn.Close()
					return nil, err
				}

				return tlsConn, nil
			}
		}
	}
}

// determineProxy resolves the effective proxy for req using three-tier priority:
//  1. Per-request [WithProxyOverride] stored in the request context.
//  2. Client-level proxy set via [WithClientProxy].
//  3. System environment variables (HTTP_PROXY / HTTPS_PROXY).
//
// It is the single source of truth for proxy resolution inside the client and
// is used by [ProxyFuncWithOverride] to configure [http.Transport.Proxy].
func (c *Client) determineProxy(req *http.Request) (*url.URL, error) {
	if raw, ok := GetProxyOverride(req.Context()).Value(); ok && raw != "" {
		return url.Parse(raw)
	}

	if c.network.ProxyAddr != nil {
		return c.network.ProxyAddr, nil
	}

	return http.ProxyFromEnvironment(req)
}

func dialTLSWithUTLS(
	ctx context.Context,
	network, addr string,
	browser BrowserID,
	helloID *utls.ClientHelloID,
	sourceRotator *SourceIPRotator,
	dnsResolver DNSResolver,
	ja4Callback func(ja4.Report),
	tlsConfig *tls.Config,
	proxyURL *url.URL,
) (net.Conn, error) {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.JA4Callback != nil {
			ja4Callback = cfg.JA4Callback
		}
	}

	ssrfGuard := false
	delay := 300 * time.Millisecond

	if cfg != nil {
		ssrfGuard = cfg.SSRFGuard
		delay = cfg.HappyEyeballsDelay
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	// Apply per-request TCP delay (WithTCPDelay) before dialing.
	if err := ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	// Check for a per-request proxy override (WithProxyOverride).
	if raw, ok := GetProxyOverride(ctx).Value(); ok && raw != "" {
		if parsed, parseErr := url.Parse(raw); parseErr == nil {
			proxyURL = parsed
		}
	}

	// Route through proxy if configured - prevents direct IP leak.
	var conn net.Conn
	if proxyURL != nil {
		conn, err = dialViaProxy(ctx, network, host, port, proxyURL)
	} else {
		conn, err = cleanDialContext(ctx, network, addr, delay, ssrfGuard, sourceRotator, dnsResolver)
	}

	if err != nil {
		return nil, err
	}

	var spec utls.ClientHelloID
	if helloID != nil {
		spec = *helloID
	} else {
		switch browser {
		case BrowserFirefox:
			spec = utls.HelloFirefox_Auto
		case BrowserSafari:
			spec = utls.HelloSafari_Auto
		default:
			spec = utls.HelloChrome_Auto
		}
	}

	uConfig := &utls.Config{
		ServerName: host,
		NextProtos: []string{"http/1.1"},
	}

	if tlsConfig != nil {
		uConfig.InsecureSkipVerify = tlsConfig.InsecureSkipVerify
		uConfig.RootCAs = tlsConfig.RootCAs
		uConfig.MinVersion = tlsConfig.MinVersion
		uConfig.MaxVersion = tlsConfig.MaxVersion
		uConfig.CipherSuites = tlsConfig.CipherSuites
		uConfig.VerifyPeerCertificate = tlsConfig.VerifyPeerCertificate

		if len(tlsConfig.CurvePreferences) > 0 {
			uConfig.CurvePreferences = make([]utls.CurveID, len(tlsConfig.CurvePreferences))
			for i, id := range tlsConfig.CurvePreferences {
				uConfig.CurvePreferences[i] = utls.CurveID(id)
			}
		}
	}

	// Per-request InsecureSkipVerify override (WithInsecureSkipVerify).
	if GetInsecureSkipVerify(ctx) {
		uConfig.InsecureSkipVerify = true //nolint:gosec
	}

	// Inject certificate pin verification if configured.
	if cfg != nil && len(cfg.CertificatePins) > 0 {
		userVerify := uConfig.VerifyPeerCertificate
		uConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if err := verifyCertificatePins(host, cfg.CertificatePins, rawCerts); err != nil {
				return err
			}

			if userVerify != nil {
				return userVerify(rawCerts, verifiedChains)
			}

			return nil
		}
	}

	// Use proxy-aware session cache if available in context.
	if cfg != nil && cfg.SessionCache != nil {
		uConfig.ClientSessionCache = cfg.SessionCache
	}

	if cfg != nil && len(cfg.ALPNOverride) > 0 {
		uConfig.NextProtos = cfg.ALPNOverride
	}

	var customSpec *utls.ClientHelloSpec
	if cfg != nil && cfg.ClientHelloSpecProvider != nil {
		var err error

		customSpec, err = cfg.ClientHelloSpecProvider.ClientHelloSpec()
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("aoni tls: failed to get custom client hello spec: %w", err)
		}
	}

	var uConn *utls.UConn
	if customSpec != nil {
		uConn = utls.UClient(conn, uConfig, utls.HelloCustom)
		if err := uConn.ApplyPreset(customSpec); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("aoni tls: failed to apply custom client hello spec: %w", err)
		}
	} else {
		uConn = utls.UClient(conn, uConfig, spec)
	}

	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	alpnProtos := []string{"http/1.1"}
	if cfg != nil && len(cfg.ALPNOverride) > 0 {
		alpnProtos = cfg.ALPNOverride
	}

	uConn.Extensions = forceALPN(uConn.Extensions, alpnProtos)

	report := extractJA4FromUConn(uConn, host)

	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Write JA4 report to the store in the request context (set by TraceJA4).
	// The request context flows through to DialTLSContext.
	if cfg != nil && cfg.JA4ReportStore != nil {
		cfg.JA4ReportStore.report = &report
	}

	if ja4Callback != nil {
		ja4Callback(report)
	}

	return uConn, nil
}

// extractJA4FromUConn computes a JA4 fingerprint from a uTLS connection after handshake.
func extractJA4FromUConn(uConn *utls.UConn, _ string) ja4.Report {
	_ = uConn.BuildHandshakeState()

	hello := uConn.HandshakeState.Hello

	var (
		extensions    []uint16
		sigAlgorithms []uint16
	)

	if len(hello.Raw) > 0 {
		extensions, sigAlgorithms = ja4.ParseExtensionsFromRaw(hello.Raw)
	}

	// Convert signature algorithms to uint16
	sigAlgos := make([]uint16, len(sigAlgorithms))
	for i, s := range sigAlgorithms {
		sigAlgos[i] = uint16(s)
	}

	sni := hello.ServerName != ""
	fingerprint := ja4.ComputeJA4(
		hello.CipherSuites,
		extensions,
		hello.SupportedVersions,
		sni,
		hello.AlpnProtocols,
		sigAlgos,
	)

	report := ja4.Report{
		JA4:         fingerprint,
		Protocol:    "t",
		CipherCount: len(ja4.FilterGREASE(hello.CipherSuites)),
		ExtCount:    len(ja4.FilterGREASE(extensions)),
	}

	// Parse version from fingerprint
	if len(fingerprint) >= 4 {
		report.Version = fingerprint[1:3]
	}

	if sni {
		report.SNI = "d"
	} else {
		report.SNI = "i"
	}

	if len(hello.AlpnProtocols) > 0 && hello.AlpnProtocols[0] != "" {
		report.ALPN = string(hello.AlpnProtocols[0][0]) + string(hello.AlpnProtocols[0][len(hello.AlpnProtocols[0])-1])
	} else {
		report.ALPN = "00"
	}

	return report
}

// ja4ReportStore is a shared pointer that allows dialTLSWithUTLS to write the JA4 report
// and Client.Request to copy it to the target TraceInfo after the request completes.
type ja4ReportStore struct {
	report *ja4.Report
	target *TraceInfo
}

func isCrossOrigin(u1, u2 *url.URL) bool {
	if u1.Scheme != u2.Scheme {
		return true
	}

	if u1.Host != u2.Host {
		return true
	}

	return false
}

func unwrapBody(c io.Closer) io.Closer {
	for {
		u, ok := c.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		c = u.Unwrap()
	}

	return c
}

func closeResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_ = resp.Body.Close()

	if rb, ok := unwrapBody(resp.Body).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}

	if resp.Request != nil {
		if cfg := GetRequestConfig(resp.Request.Context()); cfg != nil && cfg.RequestTimeoutCancel != nil {
			cfg.RequestTimeoutCancel()
		}
	}
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() {
		return true
	}

	// Check private IP ranges.
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 0 ||
			ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168)
	}

	if ip6 := ip.To16(); ip6 != nil {
		// Check unique local IPv6.
		return (ip6[0] & 0xfe) == 0xfc
	}

	return false
}

func wrapConn(ctx context.Context, conn net.Conn) net.Conn {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.PacketPadding != nil && cfg.PacketPadding.MaxSegmentSize > 0 {
			conn = wrapWithMSSLimit(conn, cfg.PacketPadding.MaxSegmentSize)
		}

		if len(cfg.OrderedHeaders) > 0 {
			conn = &headerOrderingConn{Conn: conn, orderedKeys: cfg.OrderedHeaders}
		}
	}

	var fCfg *FragmentConfig
	if cfg != nil && cfg.Fragment != nil {
		fCfg = cfg.Fragment
	} else if val, ok := ctx.Value(fragmentCtxKey{}).(FragmentConfig); ok {
		fCfg = &val
	}

	if fCfg != nil && fCfg.ChunkSize > 0 {
		conn = wrapWithFragmentation(conn, *fCfg)
	}

	return conn
}

func makeDialerControl(ctx context.Context) func(network, address string, rc syscall.RawConn) error {
	var (
		spoofer    *p0f.Spoofer
		controller SocketController
	)

	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		if cfg.P0fSignature != nil {
			spoofer = p0f.NewSpoofer(cfg.P0fSignature)
		}

		controller = cfg.SocketController
	}

	if spoofer == nil && controller == nil {
		return nil
	}

	return func(network, address string, rc syscall.RawConn) error {
		if controller != nil {
			var controlErr error

			err := rc.Control(func(fd uintptr) {
				controlErr = controller.Control(fd, network, address)
			})
			if err != nil {
				return err
			}

			if controlErr != nil {
				return controlErr
			}
		}

		if spoofer != nil {
			return spoofer.ApplyToRawConn(rc)
		}

		return nil
	}
}

func cleanDialContext(
	ctx context.Context,
	network, addr string,
	delay time.Duration,
	ssrfGuard bool,
	rotator *SourceIPRotator,
	dnsResolver DNSResolver,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	if rules := HostRewriteRules(ctx); len(rules) > 0 {
		if rewritten, exists := rules[host]; exists {
			if newHost, newPort, err := net.SplitHostPort(rewritten); err == nil {
				host = newHost

				if newPort != "" {
					port = newPort
				}
			}
		}
	}

	// Proxy DNS: route DNS resolution through the proxy to prevent local DNS leaks.
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.ProxyDNS {
		if cfg.ProxyAddr != nil && net.ParseIP(host) == nil {
			return dialViaProxy(ctx, network, host, port, cfg.ProxyAddr)
		}
	}

	resolver := dnsResolver
	if resolver == nil {
		resolver = &net.Resolver{}
	}

	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	// Explicitly perform SSRF checks on resolved IP addresses prior to dialing
	for _, ia := range addrs {
		if ssrfGuard && isBlockedIP(ia.IP) {
			return nil, fmt.Errorf("%w: blocked IP %s", ErrSSRFBlocked, ia.IP)
		}
	}

	// Delegate connection creation to the standard net.Dialer
	dialer := &net.Dialer{
		Timeout:       30 * time.Second,
		FallbackDelay: delay,
	}

	if rotator != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: rotator.Next()}
	}

	dialer.Control = makeDialerControl(ctx)

	conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}

	return wrapConn(ctx, conn), nil
}

// dialViaProxy connects to a target host through a SOCKS5 proxy, performing DNS
// resolution on the proxy side to prevent local DNS leaks. For HTTP CONNECT
// proxies, the proxy resolves the hostname when handling the CONNECT request.
func dialViaProxy(ctx context.Context, _, host, port string, proxyURL *url.URL) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyAddr == "" {
		return nil, errors.New("aoni: proxy DNS enabled but proxy address is empty")
	}

	if net.ParseIP(proxyAddr) == nil {
		// proxyAddr may not have a port, default to 1080 for SOCKS5
		if _, _, err := net.SplitHostPort(proxyAddr); err != nil {
			proxyAddr = net.JoinHostPort(proxyAddr, "1080")
		}
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}

	proxyConn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("aoni: dial proxy %s: %w", proxyAddr, err)
	}

	// Set a deadline for the entire handshake phase to prevent goroutine leaks.
	handshakeDeadline := time.Now().Add(30 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
	}

	if err := proxyConn.SetDeadline(handshakeDeadline); err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: set proxy handshake deadline: %w", err)
	}

	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		if err := socks5Handshake(proxyConn, host, port, proxyURL); err != nil {
			_ = proxyConn.Close()
			return nil, err
		}

		_ = proxyConn.SetDeadline(time.Time{})

		return proxyConn, nil
	}

	// HTTP CONNECT proxy: send CONNECT and let the proxy resolve DNS.
	connectReq := fmt.Sprintf("CONNECT %s:%s HTTP/1.1\r\nHost: %s:%s\r\n\r\n",
		host, port, host, port)
	if _, err := proxyConn.Write([]byte(connectReq)); err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: send CONNECT to proxy: %w", err)
	}

	br := bufio.NewReader(proxyConn)

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: read CONNECT response: %w", err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("aoni: CONNECT rejected with status %s", resp.Status)
	}

	_ = proxyConn.SetDeadline(time.Time{})

	// If bufio.Reader buffered data beyond the HTTP response, wrap the
	// connection so the leftover bytes are returned before real network data.
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: proxyConn, r: br}, nil
	}

	return proxyConn, nil
}

// bufferedConn wraps a net.Conn with a bufio.Reader so that leftover bytes
// buffered during HTTP response parsing are returned before real network data.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	if c.r.Buffered() > 0 {
		return c.r.Read(b)
	}

	return c.Conn.Read(b)
}

// socks5Handshake performs the SOCKS5 protocol handshake with remote DNS resolution.
func socks5Handshake(conn net.Conn, host, port string, proxyURL *url.URL) error {
	// Step 1: Greeting - offer both NO AUTH and USERNAME/PASSWORD when credentials exist.
	greeting := []byte{0x05, 0x01, 0x00} // VER=5, NMETHODS=1, NO AUTH
	if proxyURL.User != nil {
		greeting = []byte{0x05, 0x02, 0x00, 0x02} // VER=5, NMETHODS=2, [NO AUTH, USERNAME/PASSWORD]
	}

	if _, err := conn.Write(greeting); err != nil {
		return fmt.Errorf("aoni: socks5 greeting: %w", err)
	}

	// Step 2: Server choice
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("aoni: socks5 read choice: %w", err)
	}

	if resp[0] != 0x05 {
		return fmt.Errorf("aoni: socks5 unsupported version: %d", resp[0])
	}

	// Step 3: Authentication
	switch resp[1] {
	case 0x02: // Username/Password
		if proxyURL.User == nil {
			return errors.New("aoni: socks5 server requires auth but no credentials provided")
		}

		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()

		if len(username) > 255 || len(password) > 255 {
			return errors.New("aoni: socks5 auth credentials exceed 255 byte limit")
		}

		auth := make([]byte, 0, 2+len(username)+1+len(password))
		auth = append(auth, 0x01, byte(len(username))) //nolint:gosec
		auth = append(auth, []byte(username)...)
		auth = append(auth, byte(len(password))) //nolint:gosec
		auth = append(auth, []byte(password)...)

		if _, err := conn.Write(auth); err != nil {
			return fmt.Errorf("aoni: socks5 auth write: %w", err)
		}

		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			return fmt.Errorf("aoni: socks5 auth read: %w", err)
		}

		if authResp[1] != 0x00 {
			return fmt.Errorf("aoni: socks5 auth failed: status %d", authResp[1])
		}

	case 0x00: // No auth required
	default:
		return fmt.Errorf("aoni: socks5 unsupported auth method: %d", resp[1])
	}

	// Step 4: Connection request (remote DNS - ATYP=0x03 domain name)
	if len(host) > 255 {
		return fmt.Errorf("aoni: socks5 hostname exceeds 255 bytes: %s", host)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 0 || portNum > 65535 {
		return fmt.Errorf("aoni: socks5 invalid port: %s", port)
	}

	req := make([]byte, 0, 5+len(host)+2)
	req = append(req, 0x05, 0x01, 0x00, 0x03) //nolint:gosec // VER=5, CMD=CONNECT, RSV=0, ATYP=DOMAIN
	req = append(req, byte(len(host)))        //nolint:gosec
	req = append(req, []byte(host)...)
	req = append(req, byte(portNum>>8), byte(portNum)) //nolint:gosec

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("aoni: socks5 connect request: %w", err)
	}

	// Step 5: Read connect reply
	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("aoni: socks5 connect reply: %w", err)
	}

	if reply[1] != 0x00 {
		return fmt.Errorf("aoni: socks5 connect failed: code %d", reply[1])
	}

	// Skip the rest of the reply (bind addr + bind port)
	switch reply[3] {
	case 0x01: // IPv4
		_, _ = io.CopyN(io.Discard, conn, 4+2)
	case 0x03: // Domain
		domainLen := make([]byte, 1)
		if _, err := io.ReadFull(conn, domainLen); err != nil {
			return fmt.Errorf("aoni: socks5 read domain length: %w", err)
		}

		_, _ = io.CopyN(io.Discard, conn, int64(domainLen[0])+2)

	case 0x04: // IPv6
		_, _ = io.CopyN(io.Discard, conn, 16+2)
	}

	return nil
}

func forceALPN(extensions []utls.TLSExtension, protos []string) []utls.TLSExtension {
	found := false
	filtered := make([]utls.TLSExtension, 0, len(extensions))

	for _, ext := range extensions {
		switch ext.(type) {
		case *utls.ALPNExtension:
			filtered = append(filtered, &utls.ALPNExtension{
				AlpnProtocols: protos,
			})
			found = true
		case *utls.ApplicationSettingsExtension:
			if slices.Contains(protos, "h2") {
				filtered = append(filtered, ext)
			}
		default:
			filtered = append(filtered, ext)
		}
	}

	if !found {
		filtered = append(filtered, &utls.ALPNExtension{
			AlpnProtocols: protos,
		})
	}

	return filtered
}
