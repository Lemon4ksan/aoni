// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	stdio "io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	asyncctx "github.com/lemon4ksan/foundation/async/context"
	"github.com/lemon4ksan/foundation/async/log"
	"github.com/lemon4ksan/foundation/generic"
	foundationurl "github.com/lemon4ksan/foundation/net/url"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	utls "github.com/refraction-networking/utls"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/internal/experimental"
	"github.com/lemon4ksan/aoni/internal/pipeline"
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
	engine       HTTPDoer
	pipeline     *pipeline.Pipeline[*http.Request, *http.Response]
	engineConfig EngineConfig
	defaults     ClientDefaults
	network      NetworkConfig
	fingerprint  FingerprintConfig
	powerWatcher *power.Watcher
	referer      *pipeline.RefererState
	prepared     pipeline.PreparedConfig
	coreEngine   *pipeline.Engine
}

// NewClient instantiates a new thread-safe [Client] wrapping the specified doer engine.
// If doer is nil, defaults to standard HTTP execution normalized via [DefaultEngine].
//
// Applies functional [ClientOption] layers, precomputes BaseURL string representations
// into [engine.PreparedConfig] for zero-alloc relative path resolutions, and ensures a default User-Agent.
//
// Client instances are safe for concurrent use by multiple goroutines.
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
	generic.ApplyOptions(&cfg, opts...)

	client.applyConfig(cfg)
	client.ensureUserAgent()

	return client
}

// Clone creates a deep, memory-isolated copy of the [Client].
// All configuration DTOs, default header maps, modifier slices, cookie jars, and referer states
// are independently copied, guaranteeing zero data races when mutating cloned instances across goroutines.
func (c *Client) Clone() *Client {
	return c.With()
}

// With produces a deep-copied [Client] with the provided functional options applied,
// preserving original client immutability and thread safety.
func (c *Client) With(opts ...ClientOption) *Client {
	clonedReferer := &pipeline.RefererState{}
	if c.referer != nil {
		c.referer.Mu.Lock()
		clonedReferer.LastURL = c.referer.LastURL
		c.referer.Mu.Unlock()
	}

	cfg := c.snapshotConfig()
	generic.ApplyOptions(&cfg, opts...)

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
// Relative paths are resolved against BaseURL using precomputed zero-allocation string buffers.
// Absolute HTTP/HTTPS URLs override BaseURL directly.
//
// The caller MUST close the returned response body stream when finished.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	// Checked BEFORE any allocation. When the client has no pipeline rules, hooks,
	// modifiers, or per-request config we bypass AcquireTx, NewStdRequest and the
	// full pipeline.Execute and route directly to the underlying engine.
	if len(mods) == 0 && c.isBaremetalStaticEligible() && GetRequestConfig(ctx) == nil {
		return c.doBaremetal(ctx, method, path)
	}

	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		ApplyRequestConfigDefaults(cfg, c)
	} else if len(mods) > 0 || len(c.defaults.DefaultMods) > 0 || c.needsRequestConfig() {
		ctx, cfg = AllocRequestConfig(ctx)
		ApplyRequestConfigDefaults(cfg, c)
	}

	url, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	var reqHeader http.Header

	headerCap := len(c.prepared.PrecomputedDefaultHeaders) + len(c.defaults.Headers)
	if headerCap > 0 {
		reqHeader = make(http.Header, headerCap)
		if len(c.prepared.PrecomputedDefaultHeaders) > 0 {
			for i := range c.prepared.PrecomputedDefaultHeaders {
				h := &c.prepared.PrecomputedDefaultHeaders[i]
				reqHeader[h.Key] = h.Slice
			}
		} else if len(c.defaults.Headers) > 0 {
			for k, v := range c.defaults.Headers {
				reqHeader[k] = slices.Clone(v)
			}
		}
	}

	req := &http.Request{
		Method:     method,
		URL:        url,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     reqHeader,
		Body:       http.NoBody,
		Host:       url.Host,
	}

	if ctx != nil {
		req = req.WithContext(ctx)
	}

	for _, m := range c.defaults.DefaultMods {
		m.ApplyStd(req)
	}

	for _, m := range mods {
		m.ApplyStd(req)
	}

	resp, err := c.execute(req, c.resolvePipeline(req))
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return resp, nil
}

// doBaremetal executes a request on the minimal allocation path - bypassing AcquireTx,
// NewStdRequest, and the full pipeline. Called only when isBaremetalStaticEligible is true
// and no per-request mods or config are present.
func (c *Client) doBaremetal(ctx context.Context, method, path string) (*http.Response, error) {
	u, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	// nil Header is safe: net/http handles absent request headers correctly.
	// Avoids make(http.Header, 0) allocation on the hot baremetal path.
	req := &http.Request{
		Method:     method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     nil,
		Host:       u.Host,
	}

	// Avoid the 2-alloc req.WithContext copy for background/todo contexts with nil Done() channel
	if ctx != nil && ctx.Done() != nil {
		req = req.WithContext(ctx)
	}

	resp, err := c.engine.Do(req)
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return resp, nil
}

// Do executes a prepared [Request] contract via the client execution pipeline,
// accepting both native aoni.Request and fast.Request adapters.
//
// The caller MUST call resp.Close() on the returned response to release pooled memory.
func (c *Client) Do(req Request) (Response, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	httpReq, err := c.resolveHTTPRequest(req)
	if err != nil {
		return nil, err
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

// Close releases background janitor workers and engine resources. Safe for repeated calls.
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
	fastCtx := asyncctx.Wrap(req.Context())
	if req.Context() != fastCtx {
		req = req.WithContext(fastCtx)
	}

	return c.pipeline.Execute(fastCtx, req, c.engine, pipe.toInternal())
}

// WithPersona configures TLS ClientHello ID, HTTP/2 SETTINGS frames, header order,
// p0f OS stack signatures, and User-Agent headers matching a specific browser persona (e.g. Chrome, Firefox, Safari)
// in a single atomic clone call to prevent cross-layer fingerprint mismatches.
func (c *Client) WithPersona(p fingerprint.Persona) *Client {
	return c.With(func(cfg *Config) {
		cfg.Fingerprint.TLSClientHelloID = &p.TLSID
		cfg.Fingerprint.H2Settings = &p.H2Settings
		cfg.Fingerprint.HeaderOrder = p.HeaderOrder
		cfg.Fingerprint.P0fSignature = p.P0fSignature

		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("User-Agent", p.UserAgent)

		if len(p.HeaderOrder) > 0 {
			cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, RequestModifier{
				Kind: ModCustom,
				Fn: func(req Request) {
					GetOrInitRequestConfig(req).OrderedHeaders = p.HeaderOrder
				},
			})
		}
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

	transport.DialTLSContext = cloned.DialTLS

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

// needsRequestConfig reports whether active client defaults require attaching a RequestConfig DTO to request contexts.
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

// isBaremetalStaticEligible determines if the client configuration permits fast 0-alloc baremetal execution.
func (c *Client) isBaremetalStaticEligible() bool {
	if len(c.defaults.DefaultMods) > 0 {
		return false
	}

	if c.needsRequestConfig() {
		return false
	}

	if c.defaults.Inspector != nil || len(c.defaults.BeforeRequest) > 0 || len(c.defaults.AfterResponse) > 0 ||
		len(c.defaults.UARotationProfiles) > 0 {
		return false
	}

	if c.defaults.RefererAutomaton || c.fingerprint.PacketPadding != nil {
		return false
	}

	pipe := c.defaults.Pipeline
	if pipe.Decompress || pipe.Validate || pipe.Challenge || pipe.HAR != nil || pipe.Cache != nil ||
		pipe.Hedging != nil ||
		pipe.DPIJitter != nil {
		return false
	}

	return true
}

// ensureUserAgent guarantees a default User-Agent header is set on client request defaults.
func (c *Client) ensureUserAgent() {
	if c.defaults.Headers == nil {
		return
	}

	if c.defaults.Headers.Get("User-Agent") == "" {
		c.defaults.Headers.Set("User-Agent", DefaultUserAgent)
	}
}

// resolveURL resolves relative path against client BaseURL or parses absolute URL strings.
func (c *Client) resolveURL(path string) (*url.URL, error) {
	if (path == "" || path == "/") && c.prepared.BaseURL != nil {
		clone := *c.prepared.BaseURL
		return &clone, nil
	}

	if len(path) > 0 && path[0] == '/' && c.prepared.BaseURL != nil {
		return &url.URL{
			Scheme: c.prepared.BaseURL.Scheme,
			Host:   c.prepared.BaseURL.Host,
			Path:   path,
		}, nil
	}

	targetURLStr, resolveErr := c.resolveTargetURL(path)
	if resolveErr != nil {
		return nil, resolveErr
	}

	u, parseErr := foundationurl.Parse(targetURLStr)
	if parseErr != nil {
		return nil, &Error{Op: "failed to parse URL", Err: parseErr}
	}

	return u, nil
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

	rel, err := foundationurl.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return "", &Error{Op: "invalid path", Err: ErrInvalidPath}
	}

	return c.defaults.BaseURL.ResolveReference(rel).String(), nil
}

// resolveHTTPRequest converts a generic [Request] interface into a standard [*http.Request].
// Uses zero-allocation bytesconv string conversions for header mappings.
func (c *Client) resolveHTTPRequest(req Request) (*http.Request, error) {
	if httpReq := req.HTTPRequest(); httpReq != nil {
		return httpReq, nil
	}

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

	httpReq, err := http.NewRequestWithContext(ctx, req.Method(), req.URL(), bodyReader)
	if err != nil {
		return nil, &Error{Op: "failed to create http request", Err: err}
	}

	fastAdapter, ok := req.(interface{ FastHTTPRequest() *fasthttp.Request })
	if !ok {
		return httpReq, nil
	}

	fastReq := fastAdapter.FastHTTPRequest()
	if fastReq != nil {
		fastReq.Header.All()(func(k, v []byte) bool {
			httpReq.Header.Add(bytesconv.B2S(k), bytesconv.B2S(v))
			return true
		})

		if host := fastReq.Header.Peek("Host"); len(host) > 0 {
			httpReq.Host = bytesconv.B2S(host)
		}
	}

	return httpReq, nil
}

// buildQUICConfig constructs a [quic.Config] from client settings and migration parameters.
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

// buildQUICTLSConfig constructs a tls.Config tailored for QUIC/HTTP/3 ALPN negotiations.
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

// snapshotConfig extracts a pure data DTO copy of active client configurations.
func (c *Client) snapshotConfig() Config {
	return Config{
		Network:     c.network.Clone(),
		Fingerprint: c.fingerprint.Clone(),
		Defaults:    c.defaults.Clone(),
		Engine:      c.engineConfig,
	}
}

// applyConfig applies a Config DTO to the client instance, recreating internal engines and transport dialers.
func (c *Client) applyConfig(cfg Config) {
	c.network = cfg.Network
	c.fingerprint = cfg.Fingerprint
	c.defaults = cfg.Defaults
	c.engineConfig = cfg.Engine
	c.coreEngine = pipeline.NewEngine(cfg.Defaults.BaseURL, cfg.Defaults.Headers)
	c.prepared = c.coreEngine.Prepared

	applyEngineConfig(c, cfg.Engine)
	c.applyDialers(c.Transport())
	c.reapplyH2Settings(c.Transport())
	c.applyPowerManagement(cfg.Network.EnablePowerManagement)

	if len(cfg.Network.CPUAffinityCores) > 0 {
		experimental.ApplyCPUAffinity(cfg.Network.CPUAffinityCores)
	}

	c.pipeline = pipeline.New(
		c.toPipelineDefaults(),
		c.fingerprint.ToPipelineFingerprint(),
	)
}

// applyPowerManagement manages the lifecycle of OS power suspend/resume watchers.
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
