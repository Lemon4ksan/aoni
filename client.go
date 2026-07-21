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

// Middleware wraps an [HTTPDoer] with additional request/response
// processing logic. Pass to [Chain] to compose multiple layers.
type Middleware func(next HTTPDoer) HTTPDoer

// RequestModifier represents a function that alters an [http.Request] before execution.
// Concrete modifier implementations are located in the [github.com/lemon4ksan/aoni/mod] package.
type RequestModifier = generic.Option[*http.Request]

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

// DialTLSForWS dials a TLS connection, routing through the transport's DialTLSContext when available.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	if tr := c.Transport(); tr != nil && tr.DialTLSContext != nil {
		return tr.DialTLSContext(ctx, "tcp", addr)
	}

	if browser := c.BrowserID(); browser != BrowserNone || c.fingerprint.TLSClientHelloID != nil {
		var proxyURL *url.URL
		if c.network.TransportProxy != nil {
			proxyURL, _ = c.network.TransportProxy(&http.Request{URL: &url.URL{Host: addr}})
		}

		dialCfg := dialConfig{
			Network:       "tcp",
			Addr:          addr,
			Browser:       browser,
			HelloID:       c.fingerprint.TLSClientHelloID,
			SourceRotator: c.network.SourceRotator,
			DNSResolver:   c.network.DNSResolver,
			JA4Callback:   c.fingerprint.JA4Callback,
			ProxyURL:      proxyURL,
		}

		return c.dialTLSWithUTLS(ctx, dialCfg, c.TLSConfig(), GetRequestConfig(ctx))
	}

	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		return tr.DialContext(ctx, "tcp", addr)
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}

	return dialer.DialContext(ctx, "tcp", addr)
}

// DialPlainForWS dials a plain TCP connection, routing through the transport's DialContext when available.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	var (
		conn net.Conn
		err  error
	)

	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		conn, err = tr.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = proxyClient{}.CleanDialContext(ctx, dialConfig{
			Network:       "tcp",
			Addr:          addr,
			Delay:         c.network.HappyEyeballsDelay,
			SSRFGuard:     c.network.SSRFGuard,
			SourceRotator: c.network.SourceRotator,
			DNSResolver:   c.network.DNSResolver,
		})
	}

	if err != nil {
		return nil, err
	}

	var fCfg *FragmentConfig
	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		fCfg = cfg.Fragment
	}

	if fCfg != nil {
		conn = connWrapper{}.WithFragmentation(conn, *fCfg)
	}

	return conn, nil
}
