// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
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

// HTTPDoer executes an HTTP request transaction.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc adapts a plain function matching the HTTP execution signature to the [HTTPDoer] interface.
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do executes the underlying function against the provided HTTP request.
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Middleware decorates an [HTTPDoer] with request and response interception logic.
type Middleware func(next HTTPDoer) HTTPDoer

// RequestModifier represents a functional hook that mutates an outgoing [Request] contract prior to dispatch.
type RequestModifier = generic.Option[Request]

// ClientOption represents a functional option that configures [Client] initialization or cloning.
type ClientOption generic.Option[*Config]

// Client is an immutable, thread-safe HTTP and WebSocket client built on top of [HTTPDoer].
type Client struct {
	engine      HTTPDoer
	network     NetworkConfig
	fingerprint FingerprintConfig
	defaults    ClientDefaults

	userAgentRotationCounter uint32
	proxyFailoverCounter     uint32
}

// NewClient instantiates a new thread-safe [Client] wrapping the specified engine.
func NewClient(doer HTTPDoer, opts ...ClientOption) *Client {
	client := &Client{
		engine:   defaultEngine(doer),
		defaults: defaultClientDefaults(),
		network: NetworkConfig{
			HappyEyeballsDelay: 300 * time.Millisecond,
		},
	}

	cfg := client.snapshotConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	client.applyConfig(cfg)
	client.ensureUserAgent()

	return client
}

// With produces a deep-copied [Client] with the provided functional options applied.
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

// RegisterDecoder returns a cloned [Client] with a registered custom response decoder for the specified MIME content type.
func (c *Client) RegisterDecoder(contentType string, decoder ResponseDecoder) *Client {
	return c.WithDecoder(contentType, decoder)
}

// WithDecoder returns a cloned [Client] with a registered custom response decoder for the specified MIME content type.
func (c *Client) WithDecoder(contentType string, decoder ResponseDecoder) *Client {
	return c.With(func(cfg *Config) {
		mediaType, _, _ := strings.Cut(contentType, ";")

		norm := strings.ToLower(strings.TrimSpace(mediaType))
		if norm == "" {
			return
		}

		if cfg.Defaults.Decoders == nil {
			cfg.Defaults.Decoders = make(map[string]ResponseDecoder)
		}

		if decoder == nil {
			delete(cfg.Defaults.Decoders, norm)
		} else {
			cfg.Defaults.Decoders[norm] = decoder
		}
	})
}

// WithTLSClientHelloID returns a cloned [Client] configured with the specified uTLS ClientHello ID.
func (c *Client) WithTLSClientHelloID(id utls.ClientHelloID) *Client {
	cloned := c.Clone()
	cloned.fingerprint.TLSClientHelloID = &id

	transport := cloned.Transport()
	if transport == nil {
		return cloned
	}

	transport.DialTLSContext = cloned.newDialTLSContextFunc(transport.Proxy)

	return cloned
}

// WithPersona configures TLS ClientHello ID, HTTP/2 SETTINGS frames, header order, p0f signature, and User-Agent matching Persona.
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

	if len(p.HeaderOrder) == 0 {
		return newClient
	}

	return newClient.With(func(cfg *Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req Request) {
			GetOrInitRequestConfig(req).OrderedHeaders = p.HeaderOrder
		})
	})
}

// WithHTTP3 creates a clone of the client configured for HTTP/3 over QUIC using default migration settings.
func (c *Client) WithHTTP3() *Client {
	return c.WithHTTP3Config(nil)
}

// WithHTTP3Config creates a clone of the client configured for HTTP/3 over QUIC using custom QUIC migration parameters.
func (c *Client) WithHTTP3Config(config *QUICMigrationConfig) *Client {
	cloned := c.Clone()

	if config == nil {
		cfg := DefaultQUICMigrationConfig()
		config = &cfg
	}

	quicCfg := c.buildQUICConfig(config)
	tlsCfg := c.buildQUICTLSConfig()

	cloned.engine = &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: tlsCfg,
			QUICConfig:      quicCfg,
		},
	}

	return cloned
}

func (c *Client) buildQUICConfig(config *QUICMigrationConfig) *quic.Config {
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

	if h3s := c.fingerprint.H3Settings; h3s != nil {
		quicCfg.InitialStreamReceiveWindow = h3s.InitialStreamReceiveWindow
		quicCfg.MaxStreamReceiveWindow = h3s.MaxStreamReceiveWindow
		quicCfg.InitialConnectionReceiveWindow = h3s.InitialConnectionReceiveWindow
		quicCfg.MaxConnectionReceiveWindow = h3s.MaxConnectionReceiveWindow
		quicCfg.MaxIncomingStreams = h3s.MaxIncomingStreams
		quicCfg.MaxIncomingUniStreams = h3s.MaxIncomingUniStreams
		quicCfg.EnableDatagrams = h3s.EnableDatagrams
	}

	return quicCfg
}

func (c *Client) buildQUICTLSConfig() *tls.Config {
	tlsCfg := &tls.Config{
		NextProtos: []string{AlpnH3},
	}

	if spec := c.fingerprint.TLSQUICClientHelloSpec; spec != nil && len(spec.CipherSuites) > 0 {
		tlsCfg.CipherSuites = spec.CipherSuites
	}

	return tlsCfg
}

// Clone creates a deep copy of the client, isolating transports, cookie jars, and configuration structs.
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

// Request executes an HTTP transaction and yields the response stream.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
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

	for _, m := range c.defaults.DefaultMods {
		if m != nil {
			m(stdReq)
		}
	}

	for _, m := range mods {
		if m != nil {
			m(stdReq)
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

// Do executes a prepared [Request] contract via the client execution pipeline.
func (c *Client) Do(req Request) (Response, error) {
	if req == nil {
		return nil, ErrNilRequest
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

	resp, err := c.execute(httpReq, c.resolvePipeline(httpReq)) //nolint:bodyclose
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
		len(c.defaults.Decoders) > 0 ||
		c.defaults.MultiReadThreshold > 0 ||
		c.network.SSRFGuard ||
		c.network.ProxyAddr != nil
}

// DialTLSForWS establishes an encrypted TLS socket connection for WebSockets using active uTLS profiles.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	tr := c.Transport()
	if tr != nil && tr.DialTLSContext != nil {
		return tr.DialTLSContext(ctx, "tcp", addr)
	}

	dialTLS := c.newDialTLSContextFunc(c.network.TransportProxy)

	return dialTLS(ctx, "tcp", addr)
}

// DialPlainForWS establishes a raw TCP socket connection applying active proxy and SSRF guards.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	tr := c.Transport()
	if tr != nil && tr.DialContext != nil {
		conn, err := tr.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}

		return c.applyWSFragmentation(ctx, conn), nil
	}

	dialCtx := c.newDialContextFunc()

	conn, err := dialCtx(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	return c.applyWSFragmentation(ctx, conn), nil
}

func (c *Client) applyWSFragmentation(ctx context.Context, conn net.Conn) net.Conn {
	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		return applyFragmentation(conn, *cfg.Fragment)
	}

	return conn
}

// Engine yields the underlying, undecorated [HTTPDoer] engine.
func (c *Client) Engine() HTTPDoer {
	return c.engine
}

// Defaults retrieves a clone of the client's request defaults.
func (c *Client) Defaults() ClientDefaults {
	return c.defaults.Clone()
}

// BaseResponse invokes the configured [BaseResponse] factory function if declared.
func (c *Client) BaseResponse() BaseResponse {
	if c.defaults.BaseResponse != nil {
		return c.defaults.BaseResponse()
	}

	return nil
}

// Network retrieves a clone of active network transport configurations.
func (c *Client) Network() NetworkConfig {
	return c.network.Clone()
}

// Fingerprint retrieves a clone of TLS and HTTP/2 emulation settings.
func (c *Client) Fingerprint() FingerprintConfig {
	return c.fingerprint.Clone()
}

// Inspector yields the diagnostic [TrafficInspector] if configured.
func (c *Client) Inspector() TrafficInspector {
	return c.defaults.Inspector
}

// TLSConfig returns a deep copy of the active TLS client configuration.
func (c *Client) TLSConfig() *tls.Config {
	if tr := c.Transport(); tr != nil && tr.TLSClientConfig != nil {
		return tr.TLSClientConfig.Clone()
	}

	return nil
}

// BrowserID inspects active TLS dialers to deduce the active [BrowserID] profile.
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

// Logger returns the configured diagnostic [Logger], or a no-op discard fallback.
func (c *Client) Logger() Logger {
	if c.defaults.Logger == nil {
		return log.Discard
	}

	return c.defaults.Logger
}

// HTTP returns an [HTTPDoer] adapter executing requests through the full pipeline.
func (c *Client) HTTP() HTTPDoer {
	return DoerFunc(func(req *http.Request) (*http.Response, error) {
		return c.execute(req, c.resolvePipeline(req))
	})
}

// Transport retrieves the underlying [*http.Transport] from the engine.
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

// InitRequestConfig attaches or retrieves a pooled [RequestConfig] on the request context.
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

// CloseIdleConnections closes all idle keep-alive connections maintained in the pool.
func (c *Client) CloseIdleConnections() {
	if httpClient, ok := c.engine.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}
}

// GetOrInitRequestConfig retrieves or allocates a [RequestConfig] associated with the provided target.
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

// CloneHTTPClient produces a deep copy of an [*http.Client] and its transport wrappers.
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

// CloseResponse closes the response body stream and recycles associated request context resources.
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

	if c.fingerprint.H2Settings == nil {
		return
	}

	framed := h2.NewFramedTransport(tr, *c.fingerprint.H2Settings)

	httpClient, ok := c.engine.(*http.Client)
	if !ok {
		return
	}

	if cjTrans, ok := httpClient.Transport.(*cookie.Transport); ok {
		cjTrans.Next = framed
	} else {
		httpClient.Transport = framed
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
		return "", &Error{Op: "invalid path", Err: ErrInvalidPath}
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
