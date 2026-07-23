// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/internal/io"
)

var (
	requestConfigPool = sync.Pool{
		New: func() any {
			return &RequestConfig{}
		},
	}
	defaultAcceptEncoding = []string{"zstd, br, gzip"}
)

// HTTPDoer wraps the execution of an HTTP request.
//
// This interface allows custom network stacks, mock testing harnesses,
// or middleware-wrapped clients to be executed uniformly.
// The standard [*http.Client] satisfies this contract out of the box.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc adapts a plain function to the [HTTPDoer] interface.
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do executes the underlying function against the provided HTTP request.
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Middleware executes request/response interception logic around an [HTTPDoer].
//
// Multiple middleware layers can be composed using the Chain utility to inject
// retries, circuit breaking, logging, or custom telemetry.
type Middleware func(next HTTPDoer) HTTPDoer

// RequestModifier defines a hook to alter an outgoing [Request] prior to execution.
//
// Concrete implementations (like modifying headers, cookies, or queries)
// are provided in the [github.com/lemon4ksan/aoni/mod] package.
type RequestModifier = generic.Option[Request]

// ClientOption represents a functional option utilized to customize client configuration.
//
// Options are consumed by [NewClient] or [Client.With] to alter transport timeouts,
// browser fingerprint choices, or DNS settings.
type ClientOption generic.Option[*Config]

// Client is a thread-safe, concurrency-ready HTTP and WebSocket client built on [HTTPDoer].
//
// To enforce safety across concurrent goroutines, Client structures are immutable.
// Every configuration adjustment (such as .With* methods) produces a new cloned
// client instance, leaving the parent instance untouched and safe for reuse.
type Client struct {
	engine      HTTPDoer
	network     NetworkConfig
	fingerprint FingerprintConfig
	defaults    ClientDefaults

	userAgentRotationCounter uint32
	proxyFailoverCounter     uint32
}

// NewClient instantiates a new configured [Client] wrapping the provided engine.
//
// If the engine argument is nil, a default [*http.Client] is instantiated with
// a 15-second timeout and a standard 10-hop redirect policy. It applies all
// functional options to generate the final network, fingerprint, and transport dials.
func NewClient(doer HTTPDoer, opts ...ClientOption) *Client {
	c := &Client{
		engine:   defaultEngine(doer),
		defaults: defaultClientDefaults(),
		network: NetworkConfig{
			HappyEyeballsDelay: 300 * time.Millisecond,
		},
	}

	cfg := c.snapshotConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	c.applyConfig(cfg)
	c.ensureUserAgent()

	return c
}

// With returns a clone of c with the specified functional options applied.
//
// It works in three phases:
//  1. Assemble the current client state into a Config (deep-copied).
//  2. Apply every ClientOption to that Config.
//  3. Build a new Client from the updated Config and apply transport-level side-effects.
func (c *Client) With(opts ...ClientOption) *Client {
	cfg := c.snapshotConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cloned := &Client{
		engine: c.engine,
	}
	if httpClient, ok := cloned.engine.(*http.Client); ok {
		cloned.engine = CloneHTTPClient(httpClient)
	}

	cloned.applyConfig(cfg)

	return cloned
}

// WithTLSClientHelloID returns a clone of c that uses the specified uTLS ClientHelloID
// for TLS ClientHello emulation. Only effective when the underlying [HTTPDoer]
// is an [http.Client] with an [http.Transport].
func (c *Client) WithTLSClientHelloID(id utls.ClientHelloID) *Client {
	new := c.Clone()
	new.fingerprint.TLSClientHelloID = &id

	if transport := new.Transport(); transport != nil {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialCfg := new.resolveDialConfig(ctx, network, addr)
			if dialCfg.ProxyURL == nil && transport.Proxy != nil {
				dialCfg.ProxyURL, _ = transport.Proxy(&http.Request{URL: &url.URL{Host: addr}})
			}

			return new.dialTLSWithUTLS(ctx, dialCfg)
		}
	}

	return new
}

// WithPersona returns a clone of c configured with all parameters of the target Persona.
// This sets up TLS ClientHello ID, HTTP/2 framed settings, default User-Agent,
// header serialization order, and p0f TCP spoofing signature in a single call,
// ensuring complete fingerprint consistency across all network layers.
func (c *Client) WithPersona(p fingerprint.Persona) *Client {
	newClient := c.WithTLSClientHelloID(p.TLSID)
	newClient.fingerprint.H2Settings = &p.H2Settings
	newClient.fingerprint.HeaderOrder = p.HeaderOrder
	newClient.fingerprint.P0fSignature = p.P0fSignature

	if transport := newClient.Transport(); transport != nil {
		framed := h2.NewFramedTransport(transport, p.H2Settings, p.HeaderOrder...)
		if httpClient, ok := newClient.engine.(*http.Client); ok {
			httpClient.Transport = framed
		}
	}

	newClient = newClient.With(func(cfg *Config) {
		cfg.Defaults.Headers.Set("User-Agent", p.UserAgent)
	})

	if len(p.HeaderOrder) > 0 {
		newClient = newClient.With(func(cfg *Config) {
			cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req Request) {
				GetOrInitRequestConfig(req).OrderedHeaders = p.HeaderOrder
			})
		})
	}

	return newClient
}

// WithHTTP3 returns a clone of c configured to execute transactions over HTTP/3 (QUIC)
// using production-ready default migration configurations.
func (c *Client) WithHTTP3() *Client {
	return c.WithHTTP3Config(nil)
}

// WithHTTP3Config returns a clone of c configured to execute transactions over HTTP/3 (QUIC)
// using custom QUIC Connection Migration settings.
func (c *Client) WithHTTP3Config(config *QUICMigrationConfig) *Client {
	newClient := c.Clone()

	if config == nil {
		cfg := DefaultQUICMigrationConfig()
		config = &cfg
	}

	quicCfg := &quic.Config{
		EnableDatagrams:         true,
		DisablePathMTUDiscovery: config.DisablePathMTUDiscovery,
		InitialPacketSize:       config.InitialPacketSize,
	}

	if config.KeepAlivePeriod > 0 {
		quicCfg.KeepAlivePeriod = config.KeepAlivePeriod
	}

	if config.MaxIdleTimeout > 0 {
		quicCfg.MaxIdleTimeout = config.MaxIdleTimeout
	}

	if c.fingerprint.H3Settings != nil {
		quicCfg.InitialStreamReceiveWindow = c.fingerprint.H3Settings.InitialStreamReceiveWindow
		quicCfg.MaxStreamReceiveWindow = c.fingerprint.H3Settings.MaxStreamReceiveWindow
		quicCfg.InitialConnectionReceiveWindow = c.fingerprint.H3Settings.InitialConnectionReceiveWindow
		quicCfg.MaxConnectionReceiveWindow = c.fingerprint.H3Settings.MaxConnectionReceiveWindow
		quicCfg.MaxIncomingStreams = c.fingerprint.H3Settings.MaxIncomingStreams
		quicCfg.MaxIncomingUniStreams = c.fingerprint.H3Settings.MaxIncomingUniStreams
		quicCfg.EnableDatagrams = c.fingerprint.H3Settings.EnableDatagrams
	}

	tlsCfg := &tls.Config{
		NextProtos: []string{"h3"},
	}

	if spec := c.fingerprint.TLSQUICClientHelloSpec; spec != nil {
		if len(spec.CipherSuites) > 0 {
			tlsCfg.CipherSuites = spec.CipherSuites
		}
	}

	rt := &http3.Transport{
		TLSClientConfig: tlsCfg,
		QUICConfig:      quicCfg,
	}

	newClient.engine = &http.Client{
		Transport: rt,
	}

	return newClient
}

// Clone returns a deep copy of c. The cloned client shares nothing
// mutable with the original - transport, cookie jar, and config
// structs are all independently copied.
func (c *Client) Clone() *Client {
	cloned := &Client{
		engine: c.engine,
	}
	if httpClient, ok := cloned.engine.(*http.Client); ok {
		cloned.engine = CloneHTTPClient(httpClient)
	}

	cfg := c.snapshotConfig()
	cloned.applyConfig(cfg)

	return cloned
}

// Request executes a parameterized HTTP transaction and yields the response.
//
// Relative paths are resolved against the client's configured BaseURL.
// The transaction applies decompression (br, zstd, gzip) and automatically
// transcodes non-UTF-8 charsets.
//
// Preconditions:
//   - If SSRF protection is enabled and the target resolves to a private IP,
//     the method fails with [ErrSSRFBlocked].
//   - If the response payload size exceeds the configured cap, execution
//     aborts with [ErrResponseTooLarge].
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	var targetURLStr string

	targetURLStr, err := c.resolveTargetURL(path)
	if err != nil {
		return nil, err
	}

	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		cfg.ApplyDefaults(c)
	} else if len(mods) > 0 || len(c.defaults.DefaultMods) > 0 || c.needsRequestConfig() {
		cfg = requestConfigPool.Get().(*RequestConfig)
		ctx = context.WithValue(ctx, requestConfigKey{}, cfg)
		cfg.ApplyDefaults(c)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURLStr, http.NoBody) //nolint:gosec
	if err != nil {
		return nil, &Error{Op: "failed to create request", Err: err}
	}

	if len(c.defaults.Headers) > 0 {
		maps.Copy(req.Header, c.defaults.Headers)
	}

	if len(req.Header["Accept-Encoding"]) == 0 {
		req.Header["Accept-Encoding"] = defaultAcceptEncoding
	}

	stdReq := NewStdRequest(req)

	if len(c.defaults.DefaultMods) > 0 {
		for _, m := range c.defaults.DefaultMods {
			if m != nil {
				m(stdReq)
			}
		}
	}

	if len(mods) > 0 {
		for _, m := range mods {
			if m != nil {
				m(stdReq)
			}
		}
	}

	if cfg != nil {
		if cfg.BodyError != nil {
			return nil, &Error{Op: "body encoding failed", Err: cfg.BodyError}
		}

		if cfg.QueryError != nil {
			return nil, &Error{Op: "query encoding failed", Err: cfg.QueryError}
		}
	}

	resp, err := c.execute(req, c.resolvePipeline(req))
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return resp, nil
}

// Do executes a prepared [Request] contract via the client's pipeline.
//
// Provides complete method parity with fast.Client, allowing standard and fast engines
// to execute unified [Request] adapters interchangeably.
func (c *Client) Do(req Request) (Response, error) {
	if req == nil {
		return nil, errors.New("aoni: request is nil")
	}

	httpReq := req.HTTPRequest()
	if httpReq == nil {
		ctx := req.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		var err error
		httpReq, err = http.NewRequestWithContext(ctx, req.Method(), req.URL(), req.BodyStream())
		if err != nil {
			return nil, &Error{Op: "failed to create http request", Err: err}
		}
	}

	resp, err := c.execute(httpReq, c.resolvePipeline(httpReq))
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return NewStdResponse(resp), nil
}

func (c *Client) needsRequestConfig() bool {
	return c.network.SocketController != nil ||
		c.fingerprint.TLSClientHelloSpecProvider != nil ||
		len(c.fingerprint.CertificatePins) > 0 ||
		c.fingerprint.P0fSignature != nil ||
		c.fingerprint.JA4Callback != nil ||
		c.defaults.QueryEncoder != nil ||
		c.defaults.MultiReadThreshold > 0 ||
		c.network.SSRFGuard ||
		c.network.ProxyAddr != nil
}

// DialTLSForWS establishes an encrypted TLS socket connection.
//
// The connection respects Happy Eyeballs stagger, proxy setups, SSRF safeguards,
// and applies the configured browser-grade uTLS ClientHello signature.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	tr := c.Transport()
	if tr != nil && tr.DialTLSContext != nil {
		return tr.DialTLSContext(ctx, "tcp", addr)
	}

	if browser := c.BrowserID(); browser != BrowserNone || c.fingerprint.TLSClientHelloID != nil {
		dialCfg := c.resolveDialConfig(ctx, "tcp", addr)
		if dialCfg.ProxyURL == nil && c.network.TransportProxy != nil {
			dialCfg.ProxyURL, _ = c.network.TransportProxy(&http.Request{URL: &url.URL{Host: addr}})
		}

		return c.dialTLSWithUTLS(ctx, dialCfg)
	}

	if tr != nil && tr.DialContext != nil {
		return tr.DialContext(ctx, "tcp", addr)
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}

	return dialer.DialContext(ctx, "tcp", addr)
}

// DialPlainForWS establishes a raw TCP socket connection.
//
// It routes traffic through the transport's DialContext when configured,
// falling back to direct dialers with active SSRF, host rewrites, and Happy Eyeballs checks.
// Applies packet fragmentation if configured.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	var (
		conn net.Conn
		err  error
	)

	tr := c.Transport()
	if tr != nil && tr.DialContext != nil {
		conn, err = tr.DialContext(ctx, "tcp", addr)
	} else {
		dialCfg := c.resolveDialConfig(ctx, "tcp", addr)
		conn, err = proxyClient{}.CleanDialContext(ctx, dialCfg)
	}

	if err != nil {
		return nil, err
	}

	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		conn = applyFragmentation(conn, *cfg.Fragment)
	}

	return conn, nil
}

// Engine exposes the underlying, undecorated [HTTPDoer] engine.
func (c *Client) Engine() HTTPDoer {
	return c.engine
}

// Defaults retrieves the default request settings configured on this client.
func (c *Client) Defaults() ClientDefaults {
	return c.defaults.Clone()
}

// BaseResponse invokes the configured BaseResponse factory function if present.
func (c *Client) BaseResponse() BaseResponse {
	if c.defaults.BaseResponse != nil {
		return c.defaults.BaseResponse()
	}

	return nil
}

// Network retrieves the active network layer configurations.
func (c *Client) Network() NetworkConfig {
	return c.network.Clone()
}

// Fingerprint retrieves the current TLS and HTTP/2 emulation configurations.
func (c *Client) Fingerprint() FingerprintConfig {
	return c.fingerprint.Clone()
}

// Inspector retrieves the dashboard diagnostic traffic capturer, if configured.
func (c *Client) Inspector() TrafficInspector {
	return c.defaults.Inspector
}

// TLSConfig returns a deep copy of the client's current TLS client configurations,
// or nil if the transport does not support TLS configuration.
func (c *Client) TLSConfig() *tls.Config {
	if tr := c.Transport(); tr != nil && tr.TLSClientConfig != nil {
		return tr.TLSClientConfig.Clone()
	}

	return nil
}

// BrowserID retrieves the active browser emulation profile ID.
//
// If no explicit BrowserID is set, the method checks the underlying transport's
// TLS dialers to deduce whether Chrome emulation is active.
func (c *Client) BrowserID() BrowserID {
	if c.fingerprint.BrowserID != BrowserNone {
		return c.fingerprint.BrowserID
	}

	httpClient, ok := c.engine.(*http.Client)
	if !ok || httpClient.Transport == nil {
		return BrowserNone
	}

	tr, ok := httpClient.Transport.(*http.Transport)
	if ok && tr.DialTLSContext != nil {
		return BrowserChrome
	}

	return BrowserNone
}

// Logger returns the configured diagnostic logger, or a no-op fallback.
func (c *Client) Logger() Logger {
	if c.defaults.Logger == nil {
		return log.Discard
	}

	return c.defaults.Logger
}

// HTTP returns an [HTTPDoer] that processes requests through the client's
// full middleware and decompression pipeline.
func (c *Client) HTTP() HTTPDoer {
	return DoerFunc(func(req *http.Request) (*http.Response, error) {
		return c.execute(req, c.resolvePipeline(req))
	})
}

// Transport retrieves the underlying [*http.Transport] from the engine.
//
// If the engine is an [*http.Client] lacking a transport, a default transport
// is initialized with sensible pool timeouts. Returns nil if the underlying
// engine is a custom, non-standard HTTPDoer.
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

// InitRequestConfig initializes the request configuration for the given request.
func (c *Client) InitRequestConfig(req *http.Request) *http.Request {
	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = requestConfigPool.Get().(*RequestConfig)
		ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
		req = req.WithContext(ctx)
	}

	cfg.ApplyDefaults(c)

	return req
}

// CloseIdleConnections closes any idle keep-alive connections maintained by the client.
// This only works if the underlying [HTTPDoer] is an [http.Client].
func (c *Client) CloseIdleConnections() {
	if httpClient, ok := c.engine.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}
}

// GetOrInitRequestConfig retrieves or initializes the [RequestConfig] associated with the request context.
func GetOrInitRequestConfig(v any) *RequestConfig {
	switch req := v.(type) {
	case Request:
		if req == nil {
			return &RequestConfig{}
		}

		cfg := GetRequestConfig(req.Context())
		if cfg == nil {
			cfg = requestConfigPool.Get().(*RequestConfig)
			ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
			req.SetContext(ctx)
		}

		return cfg

	case *http.Request:
		if req == nil {
			return &RequestConfig{}
		}

		cfg := GetRequestConfig(req.Context())
		if cfg == nil {
			cfg = requestConfigPool.Get().(*RequestConfig)
			ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
			*req = *req.WithContext(ctx)
		}

		return cfg

	case context.Context:
		cfg := GetRequestConfig(req)
		if cfg == nil {
			cfg = requestConfigPool.Get().(*RequestConfig)
		}

		return cfg

	default:
		return &RequestConfig{}
	}
}

// CloneHTTPClient returns a deep cloned http client.
func CloneHTTPClient(c *http.Client) *http.Client {
	cloned := *c
	baseTr := cloned.Transport

	var wrappedJar *cookie.ProxyIsolatedJar
	if cjTr, ok := baseTr.(*cookie.Transport); ok {
		wrappedJar = cjTr.CookieJar
		baseTr = cjTr.Next
	}

	var framedTr *h2.FramedTransport
	if ft, ok := baseTr.(*h2.FramedTransport); ok {
		framedTr = ft
		baseTr = ft.Transport
	}

	if tr, ok := baseTr.(*http.Transport); ok && tr != nil {
		baseTr = tr.Clone()
	}

	if framedTr != nil {
		if tr, ok := baseTr.(*http.Transport); ok {
			baseTr = framedTr.Clone(tr)
		}
	}

	if wrappedJar != nil {
		cloned.Transport = &cookie.Transport{
			Next:      baseTr,
			CookieJar: wrappedJar,
		}
	} else {
		cloned.Transport = baseTr
	}

	return &cloned
}

// CloseResponse closes the response body and cancels the request timeout if applicable.
func CloseResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_ = resp.Body.Close()

	if rb, ok := io.UnwrapBody(resp.Body).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}

	if resp.Request == nil {
		return
	}

	cfg := GetRequestConfig(resp.Request.Context())
	if cfg == nil {
		return
	}

	if cfg.RequestTimeoutCancel != nil {
		cfg.RequestTimeoutCancel()
	}

	*cfg = RequestConfig{}
	requestConfigPool.Put(cfg)
}

func defaultEngine(doer HTTPDoer) HTTPDoer {
	if doer != nil {
		if httpClient, ok := doer.(*http.Client); ok {
			return CloneHTTPClient(httpClient)
		}

		return doer
	}

	return &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: DefaultRedirectPolicy(10),
	}
}

func defaultClientDefaults() ClientDefaults {
	return ClientDefaults{
		BaseURL:         &url.URL{},
		Headers:         make(http.Header),
		MaxResponseSize: 10 * 1024 * 1024,
		RefererState:    &RefererState{},
		Pipeline: PipelineConfig{
			Decompress: true,
			Validate:   true,
			Challenge:  true,
		},
	}
}

// reapplyH2Settings configures the low-level HTTP/2 frame transport overlays.
func (c *Client) reapplyH2Settings(tr *http.Transport) {
	if tr == nil {
		return
	}

	if c.fingerprint.H2Configurer != nil {
		t2, err := http2.ConfigureTransports(tr)
		if err == nil && t2 != nil {
			t2.TLSClientConfig = tr.TLSClientConfig
			_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
		}
	}

	if c.fingerprint.H2Settings != nil {
		framed := h2.NewFramedTransport(tr, *c.fingerprint.H2Settings)
		if httpClient, ok := c.engine.(*http.Client); ok {
			if cjTrans, ok := httpClient.Transport.(*cookie.Transport); ok {
				cjTrans.Next = framed
			} else {
				httpClient.Transport = framed
			}
		}
	}
}

func (c *Client) snapshotConfig() Config {
	return Config{
		Network:     c.network.Clone(),
		Fingerprint: c.fingerprint.Clone(),
		Defaults:    c.defaults.Clone(),
	}
}

func (c *Client) applyConfig(cfg Config) {
	c.network = cfg.Network
	c.fingerprint = cfg.Fingerprint
	c.defaults = cfg.Defaults

	applyEngineConfig(c, cfg.Engine)
	c.applyDialers(c.Transport())
	c.reapplyH2Settings(c.Transport())
}

func (c *Client) ensureUserAgent() {
	if c.defaults.Headers.Get("User-Agent") == "" {
		c.defaults.Headers.Set("User-Agent", DefaultUserAgent)
	}
}

// applyEngineConfig applies the engine-level overrides stored in [EngineConfig]
// to an already-constructed *Client. It is called by [Client.With] and [NewClient]
// after the client's data fields have been set.
func applyEngineConfig(c *Client, eng EngineConfig) {
	if eng.CustomEngine != nil {
		if httpClient, ok := eng.CustomEngine.(*http.Client); ok {
			c.engine = CloneHTTPClient(httpClient)
		} else {
			c.engine = eng.CustomEngine
			return
		}
	}

	httpClient, ok := c.engine.(*http.Client)
	if !ok {
		return
	}

	if eng.Timeout > 0 {
		httpClient.Timeout = eng.Timeout
	}

	applyRedirectPolicy(httpClient, eng)
	applyCookieJar(c, httpClient, eng.CookieJar)
	applyTransportOverrides(c, eng)
}

func applyRedirectPolicy(httpClient *http.Client, eng EngineConfig) {
	if eng.CheckRedirect != nil {
		httpClient.CheckRedirect = eng.CheckRedirect
		return
	}

	limit := eng.RedirectLimit
	if limit == redirectLimitUnset {
		return
	}

	switch {
	case limit == 0:
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	case limit > 0:
		httpClient.CheckRedirect = DefaultRedirectPolicy(limit)
	default:
		httpClient.CheckRedirect = DefaultRedirectPolicy(10)
	}
}

func (c *Client) resolveTargetURL(path string) (string, error) {
	if c.defaults.BaseURL == nil || c.defaults.BaseURL.Host == "" {
		return path, nil
	}

	if path == "" || path == "/" {
		if c.defaults.BaseURLString != "" {
			return c.defaults.BaseURLString, nil
		}

		return c.defaults.BaseURL.String(), nil
	}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}

	if path[0] == '/' && (c.defaults.BaseURL.Path == "" || c.defaults.BaseURL.Path == "/") {
		if c.defaults.BaseURLTrimmedString != "" {
			return c.defaults.BaseURLTrimmedString + path, nil
		}

		if c.defaults.BaseURLString != "" {
			return strings.TrimSuffix(c.defaults.BaseURLString, "/") + path, nil
		}

		uCopy := *c.defaults.BaseURL
		uCopy.Path = path

		return uCopy.String(), nil
	}

	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return "", &Error{Op: "invalid path", Err: err}
	}

	return c.defaults.BaseURL.ResolveReference(rel).String(), nil
}

func applyCookieJar(c *Client, httpClient *http.Client, jar http.CookieJar) {
	if jar == nil {
		return
	}

	httpClient.Jar = jar

	pJar, ok := jar.(*cookie.ProxyIsolatedJar)
	if !ok {
		return
	}

	c.defaults.HeadersCookieJar = jar

	baseTr := httpClient.Transport
	if baseTr == nil {
		baseTr = http.DefaultTransport
	}

	if cjTrans, ok := baseTr.(*cookie.Transport); ok {
		baseTr = cjTrans.Unwrap()
	}

	httpClient.Transport = &cookie.Transport{Next: baseTr, CookieJar: pJar}
}

func applyTransportOverrides(c *Client, eng EngineConfig) {
	tr := c.Transport()
	if tr == nil {
		return
	}

	if eng.InsecureSkipVerify {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}

		tr.TLSClientConfig.InsecureSkipVerify = true
	}

	if pool := eng.ConnectionPool; pool != nil {
		tr.MaxIdleConns = generic.Coalesce(pool.MaxIdleConns, tr.MaxIdleConns)
		tr.MaxIdleConnsPerHost = generic.Coalesce(pool.MaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
		tr.MaxConnsPerHost = generic.Coalesce(pool.MaxConnsPerHost, tr.MaxConnsPerHost)
		tr.IdleConnTimeout = generic.Coalesce(pool.IdleConnTimeout, tr.IdleConnTimeout)
		tr.ResponseHeaderTimeout = generic.Coalesce(pool.ResponseHeaderTimeout, tr.ResponseHeaderTimeout)
		tr.ReadBufferSize = generic.Coalesce(pool.ReadBufferSize, tr.ReadBufferSize)
		tr.WriteBufferSize = generic.Coalesce(pool.WriteBufferSize, tr.WriteBufferSize)
	}

	if h2Cfg := eng.HTTP2Config; h2Cfg != nil {
		if t2, err := http2.ConfigureTransports(tr); err == nil && t2 != nil {
			t2.ReadIdleTimeout = h2Cfg.ReadIdleTimeout
			t2.PingTimeout = h2Cfg.PingTimeout
			t2.AllowHTTP = h2Cfg.AllowHTTP
		}
	}
}

var _ RequestDoer = (*Client)(nil)
