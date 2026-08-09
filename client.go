// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	stdio "io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/log"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	utls "github.com/refraction-networking/utls"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/internal/engine"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/urlcache"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/power"
)

// Client is an immutable, thread-safe, multi-protocol HTTP, WebSockets, and gRPC client facade.
// It acts as a high-level public interface hiding low-level protocol orchestration
// (uTLS fingerprints, HTTP/2-3 framing, proxy rotation, anti-DPI packet fragmentation, and p0f OS spoofing).
//
// Designed around Progressive Disclosure of Complexity: simple REST API calls execute
// with 0-alloc fast-path performance, while advanced enterprise features (speculative hedging,
// WAF challenge solvers, uTLS browser profiles, SSH/MASQUE tunneling) are available via options
// without breaking application code contracts or requiring service rewrites.
//
// Client instances are 100% thread-safe and safe for concurrent invocation across goroutines.
// Methods such as With() and Clone() return new Client instances with isolated configuration DTOs
// and memory structures, ensuring zero shared-state data races between concurrent threads.
type Client struct {
	engine         HTTPDoer
	pipelineEngine *pipeline.Pipeline
	engineConfig   EngineConfig
	defaults       ClientDefaults
	network        NetworkConfig
	fingerprint    FingerprintConfig
	powerWatcher   *power.Watcher
	referer        *pipeline.RefererState
	prepared       engine.PreparedConfig
	coreEngine     *engine.Engine
}

// NewClient instantiates a new thread-safe [Client] wrapping the specified doer engine.
// If doer is nil, defaults to standard HTTP execution normalized via [DefaultEngine].
//
// Applies functional [ClientOption] layers, precomputes BaseURL string representations
// into [engine.PreparedConfig] for zero-alloc relative path resolutions, and ensures a default User-Agent.
func NewClient(doer any, opts ...ClientOption) *Client {
	client := &Client{
		engine: DefaultEngine(doer),
		defaults: ClientDefaults{
			BaseURL:         &url.URL{},
			Headers:         make(http.Header),
			MaxResponseSize: 10 * 1024 * 1024,
			Pipeline: PipelineConfig{
				Decompress: true,
				Validate:   true,
				Challenge:  true,
			},
		},
		network: NetworkConfig{
			HappyEyeballsDelay: 300 * time.Millisecond,
		},
		referer: &pipeline.RefererState{},
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

// Clone creates a deep, memory-isolated copy of the [Client].
// All configuration DTOs, default header maps, modifier slices, cookie jars, and referer states
// are independently copied, guaranteeing zero data races when mutating cloned instances across goroutines.
func (c *Client) Clone() *Client {
	clonedReferer := &pipeline.RefererState{}
	if c.referer != nil {
		c.referer.Mu.Lock()
		clonedReferer.LastURL = c.referer.LastURL
		c.referer.Mu.Unlock()
	}

	cloned := &Client{
		engine:  c.engine,
		referer: clonedReferer,
	}
	if httpClient, ok := cloned.engine.(*http.Client); ok {
		cloned.engine = CloneHTTPClient(httpClient)
	}

	cfg := c.snapshotConfig()
	cloned.applyConfig(cfg)

	return cloned
}

// With produces a deep-copied [Client] with the provided functional options applied.
// Preserves original client immutability and thread safety.
func (c *Client) With(opts ...ClientOption) *Client {
	clonedReferer := &pipeline.RefererState{}
	if c.referer != nil {
		c.referer.Mu.Lock()
		clonedReferer.LastURL = c.referer.LastURL
		c.referer.Mu.Unlock()
	}

	cfg := c.snapshotConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cloned := &Client{
		engine:  c.engine,
		referer: clonedReferer,
	}
	if httpClient, ok := cloned.engine.(*http.Client); ok {
		cloned.engine = CloneHTTPClient(httpClient)
	}

	cloned.applyConfig(cfg)

	return cloned
}

// Request executes an HTTP transaction using method, path, and optional modifiers,
// yielding the [*http.Response] stream.
//
// Path Resolution (RFC 3986):
// Relative paths are resolved against BaseURL using precomputed zero-allocation string buffers ([engine.PreparedConfig]).
// Absolute HTTP/HTTPS URLs override BaseURL directly.
//
// Pipeline Rules & Post-Processing:
// Transparent decompression (Gzip, Brotli, Zstd), charset transcoding to UTF-8, OOM size limits,
// and WAF challenge solving are automatically applied via pipeline rules.
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
		ApplyRequestConfigDefaults(cfg, c)
	} else if len(mods) > 0 || len(c.defaults.DefaultMods) > 0 || c.needsRequestConfig() {
		ctx, cfg = AllocRequestConfig(ctx)
		ApplyRequestConfigDefaults(cfg, c)
	}

	u, err := urlcache.Parse(targetURLStr)
	if err != nil {
		return nil, &Error{Op: "failed to parse URL", Err: err}
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), http.NoBody) //nolint:gosec
	if err != nil {
		return nil, &Error{Op: "failed to create request", Err: err}
	}

	if len(c.defaults.Headers) > 0 {
		maps.Copy(req.Header, c.defaults.Headers)
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

	resp, err := c.execute(req, c.resolvePipeline(req))

	ReleaseStdRequest(stdReq)

	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return resp, nil
}

// Do executes a prepared [Request] contract via the client execution pipeline.
// Accepts both native aoni.Request and fast.Request adapters.
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

		var bodyReader stdio.Reader
		if bs := req.BodyStream(); bs != nil {
			bodyReader = bs
		} else if bb := req.BodyBytes(); len(bb) > 0 {
			bodyReader = bytes.NewReader(bb)
		}

		var err error

		httpReq, err = http.NewRequestWithContext(ctx, req.Method(), req.URL(), bodyReader)
		if err != nil {
			return nil, &Error{Op: "failed to create http request", Err: err}
		}

		if fastAdapter, ok := req.(interface{ FastHTTPRequest() *fasthttp.Request }); ok {
			fastReq := fastAdapter.FastHTTPRequest()
			if fastReq != nil {
				fastReq.Header.All()(func(k, v []byte) bool {
					httpReq.Header.Add(string(k), string(v))
					return true
				})

				if host := string(fastReq.Header.Peek("Host")); host != "" {
					httpReq.Host = host
				}
			}
		}
	}

	if httpReq != nil && httpReq.URL != nil {
		cfg := GetOrInitRequestConfig(httpReq.Context())
		if cfg.TargetHost == "" && httpReq.URL.Hostname() != "" {
			cfg.TargetHost = httpReq.URL.Hostname()
		}
	}

	resp, err := c.execute(httpReq, c.resolvePipeline(httpReq)) //nolint:bodyclose
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return NewStdResponse(resp), nil
}

// Close releases background janitor workers and engine resources.
func (c *Client) Close() {
	if c.coreEngine != nil {
		c.coreEngine.Close()
	}

	if c.powerWatcher != nil {
		c.powerWatcher.Close()
		c.powerWatcher = nil
	}
}

// HTTP returns an [HTTPDoer] adapter executing standard *http.Request objects through the pipeline.
func (c *Client) HTTP() HTTPDoer {
	return HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		return c.execute(req, c.resolvePipeline(req))
	})
}

func (c *Client) execute(req *http.Request, pipe PipelineConfig) (*http.Response, error) {
	return c.pipelineEngine.Execute(req.Context(), NewStdRequest(req), c.engine, pipe.toInternal())
}

// WithPersona configures TLS ClientHello ID, HTTP/2 SETTINGS frames, header order,
// p0f OS stack signatures, and User-Agent headers matching a specific browser persona (e.g. Chrome, Firefox, Safari)
// in a single call to prevent cross-layer fingerprint mismatches.
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

// WithTLSClientHelloID returns a cloned [Client] configured with the specified uTLS ClientHello ID preset.
func (c *Client) WithTLSClientHelloID(id utls.ClientHelloID) *Client {
	cloned := c.Clone()
	cloned.fingerprint.TLSClientHelloID = &id

	transport := cloned.Transport()
	if transport == nil {
		return cloned
	}

	transport.DialTLSContext = cloned.DialTLSContext

	return cloned
}

// WithHTTP3 creates a clone of the client configured for HTTP/3 over QUIC (RFC 9114) using default migration settings.
func (c *Client) WithHTTP3() *Client {
	return c.WithHTTP3Config(nil)
}

// WithHTTP3Config creates a clone of the client configured for HTTP/3 over QUIC using custom QUIC migration parameters (RFC 9000 §9).
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

// Config returns a snapshot DTO copy of the active client configuration.
func (c *Client) Config() Config {
	return c.snapshotConfig()
}

// Engine yields the underlying, undecorated [HTTPDoer] execution engine.
func (c *Client) Engine() HTTPDoer {
	return c.engine
}

// Defaults retrieves a clone DTO of the client's request defaults.
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

// Network retrieves a clone DTO of active network transport configurations.
func (c *Client) Network() NetworkConfig {
	return c.network.Clone()
}

// Fingerprint retrieves a clone DTO of TLS and HTTP/2 emulation settings.
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
		var ctx context.Context

		ctx, cfg = AllocRequestConfig(req.Context())
		req = req.WithContext(ctx)
	}

	ApplyRequestConfigDefaults(cfg, c)

	return req
}

// CloseIdleConnections closes all idle keep-alive connections maintained in the pool.
func (c *Client) CloseIdleConnections() {
	if closer, ok := c.engine.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (c *Client) applyWSFragmentation(ctx context.Context, conn net.Conn) net.Conn {
	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		return applyFragmentation(conn, *cfg.Fragment)
	}

	return conn
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

func (c *Client) ensureUserAgent() {
	if c.defaults.Headers == nil {
		return
	}

	if c.defaults.Headers.Get("User-Agent") == "" {
		c.defaults.Headers.Set("User-Agent", DefaultUserAgent)
	}
}

// resolveTargetURL resolves path against BaseURL using precomputed zero-allocation string buffers (engine.PreparedConfig).
// Eliminates url.Parse and string formatting allocations for relative path resolutions on the hot path.
func (c *Client) resolveTargetURL(path string) (string, error) {
	if len(path) >= 7 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")) {
		return path, nil
	}

	if c.defaults.BaseURL == nil || c.defaults.BaseURL.Host == "" {
		return path, nil
	}

	if path == "" || path == "/" {
		if c.prepared.BaseURLString != "" {
			return c.prepared.BaseURLString, nil
		}

		return c.defaults.BaseURL.String(), nil
	}

	if path[0] == '/' && c.prepared.BaseURLTrimmedString != "" {
		return c.prepared.BaseURLTrimmedString + path, nil
	}

	rel, err := urlcache.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return "", &Error{Op: "invalid path", Err: ErrInvalidPath}
	}

	return c.defaults.BaseURL.ResolveReference(rel).String(), nil
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
		NextProtos:         []string{AlpnH3},
		ClientSessionCache: netutil.ResolveStdSessionCache(c.fingerprint.SessionCache),
	}

	if spec := c.fingerprint.TLSQUICClientHelloSpec; spec != nil && len(spec.CipherSuites) > 0 {
		tlsCfg.CipherSuites = spec.CipherSuites
	}

	return tlsCfg
}

func (c *Client) snapshotConfig() Config {
	return Config{
		Network:     c.network.Clone(),
		Fingerprint: c.fingerprint.Clone(),
		Defaults:    c.defaults.Clone(),
		Engine:      c.engineConfig,
	}
}

func (c *Client) applyConfig(cfg Config) {
	c.network = cfg.Network
	c.fingerprint = cfg.Fingerprint
	c.defaults = cfg.Defaults
	c.engineConfig = cfg.Engine
	c.coreEngine = engine.NewEngine(cfg.Defaults.BaseURL, cfg.Defaults.Headers, c.Transport(), 15*time.Second, 0)
	c.prepared = c.coreEngine.Prepared

	applyEngineConfig(c, cfg.Engine)
	c.applyDialers(c.Transport())
	c.reapplyH2Settings(c.Transport())
	c.applyPowerManagement(cfg.Network.EnablePowerManagement)

	c.pipelineEngine = pipeline.NewPipeline(
		c.toPipelineDefaults(),
		c.fingerprint.ToPipelineFingerprint(),
	)
}

func (c *Client) applyPowerManagement(enable bool) {
	if !enable {
		if c.powerWatcher != nil {
			c.powerWatcher.Close()
			c.powerWatcher = nil
		}

		return
	}

	if c.powerWatcher == nil {
		watcher := power.NewWatcher(5 * time.Second)
		watcher.OnSuspend(func() {
			c.CloseIdleConnections()
		})

		c.powerWatcher = watcher
	}
}

var _ RequestDoer = (*Client)(nil)
