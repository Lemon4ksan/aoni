// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast/h2engine"
	"github.com/lemon4ksan/aoni/internal/h1"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

type fastDialer struct {
	config        *aoni.Config
	activeTargets sync.Map
}

func newFastDialer(cfg *aoni.Config) *fastDialer {
	return &fastDialer{config: cfg}
}

// Dial executes an L4 TCP dial followed by an optional uTLS handshake.
func (d *fastDialer) Dial(addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return d.DialContext(ctx, "tcp", addr)
}

// DialContext establishes a raw L4 TCP connection using request context.
func (d *fastDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := aoni.ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	host, port, dialOpts := d.resolveDialOptions(ctx, network, addr)
	if port == "80" && !strings.Contains(addr, ":") {
		port = "443"
	}

	targetAddr := net.JoinHostPort(host, port)

	isTLS := port == "443" || d.IsHTTPSTarget(addr) || d.IsHTTPSTarget(host) || d.IsHTTPSTarget(targetAddr) ||
		(port != "80" && d.isTLSEnabled())

	rawConn, err := netdial.DialL4(ctx, network, targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	trackingConn := netutil.NewWriteTrackingConn(rawConn)

	var (
		conn            net.Conn = trackingConn
		negotiatedProto string
	)

	if isTLS {
		utlsOpts := d.resolveRTLSOptions(ctx, host)
		if len(utlsOpts.ALPNOverride) == 0 {
			utlsOpts.ALPNOverride = []string{aoni.AlpnHTTP}
		}

		uConn, report, err := netdial.HandshakeUTLS(ctx, trackingConn, host, utlsOpts)
		if err != nil {
			return nil, err
		}

		if reqCfg := aoni.GetRequestConfig(ctx); reqCfg != nil && reqCfg.JA4ReportStore != nil {
			reqCfg.JA4ReportStore.Report = &report
		}

		conn = uConn
		negotiatedProto = uConn.ConnectionState().NegotiatedProtocol
	}

	if negotiatedProto != aoni.AlpnH2 && len(d.config.Fingerprint.HeaderOrder) > 0 {
		return &h1.HeaderOrderingConn{
			Conn:        conn,
			OrderedKeys: d.config.Fingerprint.HeaderOrder,
		}, nil
	}

	return conn, nil
}

// DialTLSContext dials an L4 connection and performs a full uTLS client handshake.
func (d *fastDialer) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := aoni.ApplyTCPDelay(ctx); err != nil {
		return nil, err
	}

	host, port, dialOpts, utlsOpts := d.resolveTLSContextOptions(ctx, network, addr)
	targetAddr := net.JoinHostPort(host, port)

	if port == "80" && strings.HasSuffix(addr, ":80") {
		return netdial.DialL4(ctx, network, targetAddr, dialOpts)
	}

	rawConn, err := netdial.DialL4(ctx, network, targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	uConn, report, err := netdial.HandshakeUTLS(ctx, rawConn, host, utlsOpts)
	if err != nil {
		return nil, err
	}

	if reqCfg := aoni.GetRequestConfig(ctx); reqCfg != nil && reqCfg.JA4ReportStore != nil {
		reqCfg.JA4ReportStore.Report = &report
	}

	return uConn, nil
}

// DialH2 dials an L4 connection and performs a uTLS handshake forcing ALPN "h2".
func (d *fastDialer) DialH2(ctx context.Context, addr string) (net.Conn, error) {
	host, port, dialOpts, utlsOpts := d.resolveTLSContextOptions(ctx, "tcp", addr)
	targetAddr := net.JoinHostPort(host, port)

	if port == "80" && strings.HasSuffix(addr, ":80") {
		return netdial.DialL4(ctx, "tcp", targetAddr, dialOpts)
	}

	rawConn, err := netdial.DialL4(ctx, "tcp", targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	utlsOpts.ALPNOverride = []string{aoni.AlpnH2, aoni.AlpnHTTP}

	uConn, report, err := netdial.HandshakeUTLS(ctx, rawConn, host, utlsOpts)
	if err != nil {
		return nil, err
	}

	if uConn.ConnectionState().NegotiatedProtocol != aoni.AlpnH2 {
		_ = uConn.Close()
		return nil, h2engine.ErrServerSupport
	}

	if reqCfg := aoni.GetRequestConfig(ctx); reqCfg != nil && reqCfg.JA4ReportStore != nil {
		reqCfg.JA4ReportStore.Report = &report
	}

	return uConn, nil
}

func (d *fastDialer) TrackHTTPSTarget(addr string) {
	if val, ok := d.activeTargets.Load(addr); ok {
		d.activeTargets.Store(addr, val.(int)+1)
	} else {
		d.activeTargets.Store(addr, 1)
	}
}

func (d *fastDialer) UntrackHTTPSTarget(addr string) {
	if val, ok := d.activeTargets.Load(addr); ok {
		count := val.(int) - 1
		if count <= 0 {
			d.activeTargets.Delete(addr)
		} else {
			d.activeTargets.Store(addr, count)
		}
	}
}

func (d *fastDialer) IsHTTPSTarget(addr string) bool {
	_, ok := d.activeTargets.Load(addr)
	return ok
}

// DialContext establishes a raw L4 connection applying active proxy, DNS, and anti-DPI configurations.
func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := newFastDialer(&c.config)
	return dialer.DialContext(ctx, network, addr)
}

// DialTLSContext establishes an encrypted TLS socket connection using uTLS ClientHello specifications.
func (c *Client) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := newFastDialer(&c.config)
	return dialer.DialTLSContext(ctx, network, addr)
}

// DialPlainForWS satisfies [aoni.WSDialer] by establishing a plain TCP socket for WebSocket upgrades.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := c.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	return c.applyWSFragmentation(ctx, conn), nil
}

// DialTLSForWS satisfies [aoni.WSDialer] by establishing an encrypted TLS connection for WebSocket upgrades.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	return c.DialTLSContext(ctx, "tcp", addr)
}

func (c *Client) applyWSFragmentation(ctx context.Context, conn net.Conn) net.Conn {
	if cfg := aoni.GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		return fragment.NewFragmentedConn(conn, cfg.Fragment)
	}

	if c.config.Network.FragmentConfig != nil {
		return fragment.NewFragmentedConn(conn, c.config.Network.FragmentConfig)
	}

	return conn
}

func (c *Client) applyEngineConfig() {
	if c.config.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.config.Engine.Timeout
		c.engine.WriteTimeout = c.config.Engine.Timeout
	}

	if c.config.Engine.InsecureSkipVerify {
		c.engine.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	c.engine.DisableHeaderNamesNormalizing = true
}

func (c *Client) applyCustomDialer() {
	dialer := newFastDialer(&c.config)
	c.dialer = dialer
	c.defaultDial = dialer.Dial
	c.engine.Dial = dialer.Dial
	c.engine.DialDualStack = true
}

func (c *Client) applyDefaultHeaders(req aoni.Request) {
	if fastAdapter, ok := req.(interface{ FastHTTPRequest() *fasthttp.Request }); ok {
		fastReq := fastAdapter.FastHTTPRequest()
		if fastReq != nil {
			extractUserInfoAndSetAuth(fastReq)
		}
	}

	if req.Header("Accept-Encoding") == "" {
		req.SetHeader("Accept-Encoding", "zstd, br, gzip")
	}

	if len(c.config.Defaults.Headers) == 0 {
		return
	}

	for k, vv := range c.config.Defaults.Headers {
		if req.Header(k) == "" && len(vv) > 0 {
			req.SetHeader(k, vv[0])
		}
	}
}

func (c *Client) applyModifiers(req aoni.Request, mods []aoni.RequestModifier) {
	for _, defaultMod := range c.config.Defaults.DefaultMods {
		if defaultMod != nil {
			defaultMod(req)
		}
	}

	for _, m := range mods {
		if m != nil {
			m(req)
		}
	}
}

func (c *Client) resolveTargetURL(req aoni.Request, path string) error {
	if len(path) >= 7 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")) {
		req.SetURL(path)
		return nil
	}

	if c.config.Defaults.BaseURL != nil && c.config.Defaults.BaseURL.Host != "" {
		base := c.config.Defaults.BaseURL
		basePath := strings.TrimSuffix(base.Path, "/")

		cleanPath := path
		if cleanPath != "" && cleanPath[0] != '/' {
			cleanPath = "/" + cleanPath
		}

		fullURL := base.Scheme + "://" + base.Host + basePath + cleanPath
		req.SetURL(fullURL)

		return nil
	}

	if path == "" {
		return ErrTargetURLEmpty
	}

	req.SetURL(path)

	return nil
}

func (c *Client) resolveProtocolHandler(rawURL string) http.RoundTripper {
	if len(c.config.Engine.Protocols) == 0 {
		return nil
	}

	scheme, _, ok := strings.Cut(rawURL, "://")
	if !ok {
		return nil
	}

	normScheme := strings.ToLower(strings.TrimSpace(scheme))
	if normScheme == "http" || normScheme == "https" || normScheme == "ws" || normScheme == "wss" {
		return nil
	}

	return c.config.Engine.Protocols[normScheme]
}

func (d *fastDialer) resolveDialOptions(
	ctx context.Context,
	_, addr string,
) (host, port string, dialOpts netdial.DialOptions) {
	host, port = splitHostPortDefault(addr)
	host, port = applyHostRewriteRules(ctx, host, port)

	reqCfg := aoni.GetRequestConfig(ctx)

	dialOpts = netdial.DialOptions{
		ProxyURL:           d.config.Network.ProxyAddr,
		DNSResolver:        d.config.Network.DNSResolver,
		SourceRotator:      d.config.Network.SourceRotator,
		P0fSignature:       d.config.Fingerprint.P0fSignature,
		SocketController:   d.config.Network.SocketController,
		FragmentConfig:     d.config.Network.FragmentConfig,
		HappyEyeballs:      d.config.Network.HappyEyeballsDelay,
		SSRFGuard:          d.config.Network.SSRFGuard,
		ProxyDNS:           d.config.Network.ProxyDNS,
		InsecureSkipVerify: d.config.Engine.InsecureSkipVerify,
	}

	if reqCfg != nil {
		dialOpts.SSRFGuard = reqCfg.SSRFGuard
		dialOpts.ProxyDNS = reqCfg.ProxyDNS
		dialOpts.FragmentConfig = reqCfg.Fragment

		if reqCfg.P0fSignature != nil {
			dialOpts.P0fSignature = reqCfg.P0fSignature
		}

		if reqCfg.SocketController != nil {
			dialOpts.SocketController = reqCfg.SocketController
		}

		if reqCfg.HappyEyeballsDelay > 0 {
			dialOpts.HappyEyeballs = reqCfg.HappyEyeballsDelay
		}
	}

	if rawProxy, ok := aoni.GetProxyOverride(ctx).Value(); ok && rawProxy != "" {
		if parsed, parseErr := url.Parse(rawProxy); parseErr == nil {
			dialOpts.ProxyURL = parsed
		}
	} else if reqCfg != nil && reqCfg.ProxyAddr != nil {
		dialOpts.ProxyURL = reqCfg.ProxyAddr
	}

	return host, port, dialOpts
}

func (d *fastDialer) resolveTLSContextOptions(
	ctx context.Context,
	network, addr string,
) (host, port string, dialOpts netdial.DialOptions, utlsOpts netdial.RTLSOptions) {
	host, port = splitHostPortTLS(addr)
	host, port = applyHostRewriteRules(ctx, host, port)

	reqCfg := aoni.GetRequestConfig(ctx)

	dialOpts = netdial.DialOptions{
		ProxyURL:           d.config.Network.ProxyAddr,
		DNSResolver:        d.config.Network.DNSResolver,
		SourceRotator:      d.config.Network.SourceRotator,
		P0fSignature:       d.config.Fingerprint.P0fSignature,
		SocketController:   d.config.Network.SocketController,
		FragmentConfig:     d.config.Network.FragmentConfig,
		HappyEyeballs:      d.config.Network.HappyEyeballsDelay,
		SSRFGuard:          d.config.Network.SSRFGuard,
		ProxyDNS:           d.config.Network.ProxyDNS,
		InsecureSkipVerify: d.config.Engine.InsecureSkipVerify,
	}

	if reqCfg != nil {
		dialOpts.SSRFGuard = reqCfg.SSRFGuard
		dialOpts.ProxyDNS = reqCfg.ProxyDNS
		dialOpts.FragmentConfig = reqCfg.Fragment

		if reqCfg.P0fSignature != nil {
			dialOpts.P0fSignature = reqCfg.P0fSignature
		}

		if reqCfg.SocketController != nil {
			dialOpts.SocketController = reqCfg.SocketController
		}

		if reqCfg.HappyEyeballsDelay > 0 {
			dialOpts.HappyEyeballs = reqCfg.HappyEyeballsDelay
		}
	}

	if rawProxy, ok := aoni.GetProxyOverride(ctx).Value(); ok && rawProxy != "" {
		if parsed, parseErr := url.Parse(rawProxy); parseErr == nil {
			dialOpts.ProxyURL = parsed
		}
	} else if reqCfg != nil && reqCfg.ProxyAddr != nil {
		dialOpts.ProxyURL = reqCfg.ProxyAddr
	}

	utlsOpts = d.resolveRTLSOptions(ctx, host)

	if reqCfg != nil {
		if reqCfg.ClientHelloSpecProvider != nil {
			utlsOpts.SpecProvider = reqCfg.ClientHelloSpecProvider
		}

		if reqCfg.SessionCache != nil {
			utlsOpts.SessionCache = reqCfg.SessionCache
		}

		if reqCfg.JA4Callback != nil {
			utlsOpts.JA4Callback = reqCfg.JA4Callback
		}

		if len(reqCfg.CertificatePins) > 0 {
			utlsOpts.CertificatePins = reqCfg.CertificatePins
		}

		if len(reqCfg.ALPNOverride) > 0 {
			utlsOpts.ALPNOverride = reqCfg.ALPNOverride
		}
	}

	return host, port, dialOpts, utlsOpts
}

func (d *fastDialer) resolveRTLSOptions(ctx context.Context, host string) netdial.RTLSOptions {
	reqCfg := aoni.GetRequestConfig(ctx)

	serverName := host
	if net.ParseIP(host) != nil {
		serverName = host
		if reqCfg != nil && reqCfg.TargetHost != "" && net.ParseIP(reqCfg.TargetHost) == nil {
			serverName = reqCfg.TargetHost
		} else if d.config.Defaults.BaseURL != nil && d.config.Defaults.BaseURL.Hostname() != "" && net.ParseIP(d.config.Defaults.BaseURL.Hostname()) == nil {
			serverName = d.config.Defaults.BaseURL.Hostname()
		}
	}

	utlsOpts := netdial.RTLSOptions{
		HelloID:            d.resolveHelloID(),
		SpecProvider:       d.config.Fingerprint.TLSClientHelloSpecProvider,
		SessionCache:       d.config.Fingerprint.SessionCache,
		CertificatePins:    d.config.Fingerprint.CertificatePins,
		JA4Callback:        d.config.Fingerprint.JA4Callback,
		BaseTLSConfig:      &tls.Config{ServerName: serverName},
		InsecureSkipVerify: aoni.GetInsecureSkipVerify(ctx) || d.config.Engine.InsecureSkipVerify,
	}

	if reqCfg != nil {
		if reqCfg.ClientHelloSpecProvider != nil {
			utlsOpts.SpecProvider = reqCfg.ClientHelloSpecProvider
		}

		if reqCfg.SessionCache != nil {
			utlsOpts.SessionCache = reqCfg.SessionCache
		}

		if reqCfg.JA4Callback != nil {
			utlsOpts.JA4Callback = reqCfg.JA4Callback
		}

		if len(reqCfg.CertificatePins) > 0 {
			utlsOpts.CertificatePins = reqCfg.CertificatePins
		}

		if len(reqCfg.ALPNOverride) > 0 {
			utlsOpts.ALPNOverride = reqCfg.ALPNOverride
		}
	}

	return utlsOpts
}

func (d *fastDialer) isTLSEnabled() bool {
	f := d.config.Fingerprint

	return f.BrowserID != aoni.BrowserNone ||
		f.TLSClientHelloID != nil ||
		f.TLSClientHelloSpecProvider != nil
}

func (d *fastDialer) resolveHelloID() *utls.ClientHelloID {
	f := d.config.Fingerprint

	if f.TLSClientHelloID != nil {
		return f.TLSClientHelloID
	}

	switch f.BrowserID {
	case aoni.BrowserFirefox:
		return &utls.HelloFirefox_Auto
	case aoni.BrowserSafari:
		return &utls.HelloSafari_Auto
	default:
		return &utls.HelloChrome_Auto
	}
}

func defaultFasthttpClient() *fasthttp.Client {
	return &fasthttp.Client{
		ReadTimeout:         0,
		WriteTimeout:        0,
		MaxConnsPerHost:     512,
		MaxIdleConnDuration: 90 * time.Second,
	}
}

func cloneFasthttpClient(c *fasthttp.Client) *fasthttp.Client {
	if c == nil {
		return &fasthttp.Client{}
	}

	return &fasthttp.Client{
		Transport:                     c.Transport,
		DialTimeout:                   c.DialTimeout,
		Dial:                          c.Dial,
		TLSConfig:                     c.TLSConfig,
		RetryIf:                       c.RetryIf, //nolint:staticcheck
		RetryIfErr:                    c.RetryIfErr,
		RetryIfErrUpstream:            c.RetryIfErrUpstream,
		ConfigureClient:               c.ConfigureClient,
		Name:                          c.Name,
		MaxConnsPerHost:               c.MaxConnsPerHost,
		MaxIdleConnDuration:           c.MaxIdleConnDuration,
		MaxConnDuration:               c.MaxConnDuration,
		MaxIdemponentCallAttempts:     c.MaxIdemponentCallAttempts,
		ReadBufferSize:                c.ReadBufferSize,
		WriteBufferSize:               c.WriteBufferSize,
		ReadTimeout:                   c.ReadTimeout,
		WriteTimeout:                  c.WriteTimeout,
		MaxResponseBodySize:           c.MaxResponseBodySize,
		MaxConnWaitTimeout:            c.MaxConnWaitTimeout,
		ConnPoolStrategy:              c.ConnPoolStrategy,
		NoDefaultUserAgentHeader:      c.NoDefaultUserAgentHeader,
		DialDualStack:                 c.DialDualStack,
		DisableHeaderNamesNormalizing: c.DisableHeaderNamesNormalizing,
		DisablePathNormalizing:        c.DisablePathNormalizing,
		StreamResponseBody:            c.StreamResponseBody,
	}
}

func isCustomDialerSet(engine *fasthttp.Client, defaultDial func(string) (net.Conn, error)) bool {
	if engine == nil || engine.Dial == nil {
		return false
	}

	if defaultDial == nil {
		return true
	}

	return reflect.ValueOf(engine.Dial).Pointer() != reflect.ValueOf(defaultDial).Pointer()
}

func splitHostPortTLS(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return netutil.CleanHost(addr), "443"
	}

	return netutil.CleanHost(h), p
}

func splitHostPortDefault(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return netutil.CleanHost(addr), "80"
	}

	return netutil.CleanHost(h), p
}

func applyHostRewriteRules(ctx context.Context, host, port string) (string, string) {
	rules := aoni.HostRewriteRules(ctx)
	if rewritten, exists := rules[host]; exists {
		if newHost, newPort, err := net.SplitHostPort(rewritten); err == nil {
			host = newHost

			if newPort != "" {
				port = newPort
			}
		}
	}

	return host, port
}
