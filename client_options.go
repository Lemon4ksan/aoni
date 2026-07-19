// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/p0f"
	"github.com/lemon4ksan/aoni/profiles"
	"github.com/lemon4ksan/aoni/profiles/chrome"
	"github.com/lemon4ksan/aoni/profiles/firefox"
)

// ClientOption is a functional option used to configure an aoni [Client].
type ClientOption = generic.Option[*Client]

// WithClientLogger sets the diagnostic logger for the client.
func WithClientLogger(l Logger) ClientOption {
	return func(c *Client) {
		c.defaults.Logger = l
	}
}

// WithClientModifiers registers request modifiers that run on every request
// before the middleware chain.
func WithClientModifiers(mods ...RequestModifier) ClientOption {
	return func(c *Client) {
		c.defaults.DefaultMods = append(c.defaults.DefaultMods, mods...)
	}
}

// WithClientBaseResponse sets the response provider for structured API unwrapping.
func WithClientBaseResponse(provider func() BaseResponse) ClientOption {
	return func(c *Client) {
		c.defaults.BaseResponse = provider
	}
}

// WithClientQueryEncoder configures the default query parameters encoder for the client.
func WithClientQueryEncoder(encoder QueryEncoder) ClientOption {
	return func(c *Client) {
		c.defaults.QueryEncoder = encoder
	}
}

// WithClientBaseURL configures the base URL for resolving relative request paths.
func WithClientBaseURL(raw string) ClientOption {
	return func(c *Client) {
		if raw == "" {
			c.defaults.BaseURL = &url.URL{}
			return
		}

		if !strings.HasSuffix(raw, "/") {
			raw += "/"
		}

		baseURL, err := url.Parse(raw)
		if err == nil {
			c.defaults.BaseURL = baseURL
		}
	}
}

// WithClientHeader adds a default HTTP header sent with every request.
func WithClientHeader(key, value string) ClientOption {
	return func(c *Client) {
		c.defaults.Headers.Set(key, value)
	}
}

// WithClientHeaders merges the provided map of headers into the default request headers.
func WithClientHeaders(headers map[string]string) ClientOption {
	return func(c *Client) {
		for k, v := range headers {
			c.defaults.Headers.Set(k, v)
		}
	}
}

// WithoutClientHeaders removes all default request headers.
func WithoutClientHeaders() ClientOption {
	return func(c *Client) {
		c.defaults.Headers = make(http.Header)
	}
}

// WithClientTimeout configures the request deadline. Only works when the
// underlying engine is an [http.Client].
func WithClientTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		if httpClient, ok := c.engine.(*http.Client); ok {
			cloned := *httpClient
			cloned.Timeout = d
			c.engine = &cloned
		}
	}
}

type staticSpecProvider struct {
	spec *utls.ClientHelloSpec
}

func (s staticSpecProvider) ClientHelloSpec() (*utls.ClientHelloSpec, error) {
	return s.spec, nil
}

// WithClientProfileVariant configures the TLS fingerprint, HTTP/2 setting frames,
// and default browser headers to match the provided custom browser profile variant.
func WithClientProfileVariant(variant *profiles.Variant, os profiles.OSKey) ClientOption {
	return func(c *Client) {
		if variant == nil {
			return
		}

		// Initialize base uTLS fingerprint dialer if none configured
		if c.fingerprint.BrowserID == BrowserNone {
			if variant.HelloID.Client == "Firefox" {
				WithClientTLSFingerprint(BrowserFirefox)(c)
			} else {
				WithClientTLSFingerprint(BrowserChrome)(c)
			}
		}

		if variant.HelloSpec != nil {
			c.fingerprint.TLSClientHelloSpecProvider = staticSpecProvider{spec: variant.HelloSpec}
			c.fingerprint.TLSClientHelloID = nil
		} else if variant.HelloID != (utls.ClientHelloID{}) {
			helloID := variant.HelloID
			c.fingerprint.TLSClientHelloID = &helloID
			c.fingerprint.TLSClientHelloSpecProvider = nil
		}

		var h2Settings HTTP2Settings
		if variant.ConfigureH2 != nil {
			var h2s profiles.H2Settings
			variant.ConfigureH2(&h2s)
			h2Settings = H2SettingsFromProfile(h2s)
			c.fingerprint.H2Settings = &h2Settings
		}

		// Configure H3 based on the variant HelloID or HelloSpec
		var h3Settings HTTP3Settings
		if variant.HelloID.Client == "Firefox" {
			h3Settings = FirefoxHTTP3Settings
		} else {
			h3Settings = ChromeHTTP3Settings
		}

		c.fingerprint.H3Settings = &h3Settings

		// 1. Build and set static default headers
		if variant.BuildHeaders != nil {
			for _, h := range variant.BuildHeaders(os) {
				if h.Value != "" {
					c.defaults.Headers.Set(h.Name, h.Value)
				}
			}
		}

		// Reconstruct static OrderedHeaders for GET method (fallback) if HeaderCache is provided
		isMobile := os.IsMobile()

		var getHeadersOrder []string
		if variant.HeaderCache != nil {
			enums := variant.HeaderCache.Enums(isMobile)
			if methodOrder, ok := enums["GET"]; ok {
				getHeadersOrder = make([]string, len(methodOrder))
				for h, idx := range methodOrder {
					if idx >= 0 && idx < len(getHeadersOrder) {
						getHeadersOrder[idx] = h
					}
				}

				c.fingerprint.HeaderOrder = getHeadersOrder
			}
		}

		// 2. Add dynamic request modifier for method-specific headers, boundary, and OrderedHeaders
		c.defaults.DefaultMods = append(c.defaults.DefaultMods, func(req *http.Request) {
			headersMap := make(map[string]string)
			for k, v := range req.Header {
				if len(v) > 0 {
					headersMap[k] = v[0]
				}
			}

			if variant.InsertHeaders != nil {
				variant.InsertHeaders(headersMap, req.Method)
			}

			for k, v := range headersMap {
				if v == "" {
					continue
				}

				if req.Header.Get(k) == "" {
					req.Header.Set(k, v)
				}
			}

			// Store the custom boundary string in request config for WithMultipart
			if variant.BoundaryFunc != nil {
				cfg := getOrInitRequestConfig(req)
				cfg.MultipartBoundary = variant.BoundaryFunc()
			}

			// Dynamically set OrderedHeaders based on request method if HeaderCache is provided
			if variant.HeaderCache != nil {
				enums := variant.HeaderCache.Enums(isMobile)

				methodOrder, ok := enums[req.Method]
				if !ok {
					methodOrder = enums["GET"]
				}

				ordered := make([]string, len(methodOrder))
				for h, idx := range methodOrder {
					if idx >= 0 && idx < len(ordered) {
						ordered[idx] = h
					}
				}

				cfg := getOrInitRequestConfig(req)
				cfg.OrderedHeaders = ordered
			}
		})

		// Re-apply H2 settings on the transport level if it is initialized
		if transport := c.Transport(); transport != nil {
			if c.fingerprint.H2Configurer != nil {
				t2, err := http2.ConfigureTransports(transport)
				if err == nil && t2 != nil {
					t2.TLSClientConfig = transport.TLSClientConfig
					_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
				}
			}

			if c.fingerprint.H2Settings != nil {
				framed := NewH2FramedTransport(transport, *c.fingerprint.H2Settings)
				if httpClient, ok := c.engine.(*http.Client); ok {
					httpClient.Transport = framed
				}
			}
		}
	}
}

// WithClientBrowserProfile configures the TLS fingerprint, HTTP/2 setting frames,
// and default browser headers to match the selected browser profile.
func WithClientBrowserProfile(browser BrowserID, os profiles.OSKey) ClientOption {
	var variant *profiles.Variant
	switch browser {
	case BrowserFirefox:
		if os.IsMobile() {
			variant = firefox.Mobile
		} else {
			variant = firefox.Desktop
		}

	default: // default to BrowserChrome
		if os.IsMobile() {
			variant = chrome.Mobile
		} else {
			variant = chrome.Desktop
		}
	}

	return WithClientProfileVariant(variant, os)
}

// WithClientTCPDelay sets the default TCP connection delay range for all requests.
// When a per-request [WithTCPDelay] modifier is also present, it takes precedence.
func WithClientTCPDelay(min, max time.Duration) ClientOption {
	if min > max {
		min, max = max, min
	}

	return func(c *Client) {
		c.defaults.DefaultMods = append(c.defaults.DefaultMods, func(req *http.Request) {
			getOrInitRequestConfig(req).TCPDelay = TCPDelayRange{Min: min, Max: max}
		})
	}
}

// WithClientRedirectLimit sets the maximum redirect count.
func WithClientRedirectLimit(max int) ClientOption {
	return func(c *Client) {
		if httpClient, ok := c.engine.(*http.Client); ok {
			cloned := *httpClient
			switch {
			case max == 0:
				cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				}
			case max > 0:
				cloned.CheckRedirect = DefaultRedirectPolicy(max)
			default:
				cloned.CheckRedirect = DefaultRedirectPolicy(10)
			}

			c.engine = &cloned
		}
	}
}

// WithClientLocalAddr configures the local IP address to bind outgoing connections to.
func WithClientLocalAddr(addr string) ClientOption {
	return func(c *Client) {
		if transport := c.Transport(); transport != nil {
			localAddr, err := net.ResolveIPAddr("ip", addr)
			if err == nil {
				prevDial := transport.DialContext
				transport.DialContext = func(ctx context.Context, network, raddr string) (net.Conn, error) {
					dialer := &net.Dialer{
						Timeout:   30 * time.Second,
						KeepAlive: 30 * time.Second,
					}

					host, _, splitErr := net.SplitHostPort(raddr)
					if splitErr == nil {
						if targetIP := net.ParseIP(host); targetIP != nil {
							localIsV4 := localAddr.IP.To4() != nil

							targetIsV4 := targetIP.To4() != nil
							if localIsV4 == targetIsV4 {
								dialer.LocalAddr = &net.TCPAddr{IP: localAddr.IP}
							}
						}
					}

					if prevDial != nil {
						return prevDial(ctx, network, raddr)
					}

					return dialer.DialContext(ctx, network, raddr)
				}
			}
		}
	}
}

// WithClientHedging configures the request hedging delay.
func WithClientHedging(d time.Duration) ClientOption {
	return func(c *Client) {
		c.network.HedgingDelay = d
	}
}

// WithClientDynamicHedging configures dynamic request hedging.
func WithClientDynamicHedging(config *DynamicHedgingConfig) ClientOption {
	return func(c *Client) {
		if config == nil {
			cfg := DefaultDynamicHedgingConfig()
			c.network.DynamicHedging = &cfg
		} else {
			c.network.DynamicHedging = config
		}
	}
}

// WithClientProxyAwareSessionCache enables the proxy-aware TLS session ticket cache.
func WithClientProxyAwareSessionCache() ClientOption {
	return func(c *Client) {
		c.fingerprint.SessionCache = NewProxyAwareSessionCache()
	}
}

// WithClientPacketPadding configures packet padding to obscure segments against DPI.
func WithClientPacketPadding(cfg PaddingConfig) ClientOption {
	return func(c *Client) {
		c.fingerprint.PacketPadding = &cfg
		c.applyDialers()
	}
}

// WithClientMaxResponseSize limits the maximum bytes allowed in response bodies.
func WithClientMaxResponseSize(size int64) ClientOption {
	return func(c *Client) {
		c.defaults.MaxResponseSize = size
	}
}

// WithClientSSRFGuard enables SSRF protection by blocking requests resolving to private/loopback IPs.
func WithClientSSRFGuard() ClientOption {
	return func(c *Client) {
		c.network.SSRFGuard = true
		c.applyDialers()
	}
}

// WithClientHappyEyeballs configures the staggered Happy Eyeballs delay.
func WithClientHappyEyeballs(delay time.Duration) ClientOption {
	return func(c *Client) {
		c.network.HappyEyeballsDelay = delay
		c.applyDialers()
	}
}

// WithClientMultiReadBody sets the multi-read threshold in bytes.
func WithClientMultiReadBody(threshold int64) ClientOption {
	return func(c *Client) {
		c.defaults.MultiReadThreshold = threshold
	}
}

// WithClientMultiReadDisableDisk disables disk fallbacks when multi-read cache limit is reached.
func WithClientMultiReadDisableDisk(disable bool) ClientOption {
	return func(c *Client) {
		c.defaults.MultiReadDisableDisk = disable
	}
}

// WithClientLocalAddrPool registers a list of local IP addresses to cycle through.
func WithClientLocalAddrPool(addrs []string) ClientOption {
	return func(c *Client) {
		rotator, err := NewSourceIPRotator(addrs)
		if err == nil {
			c.network.SourceRotator = rotator
			c.applyDialers()
		}
	}
}

// WithClientDNSResolver sets the resolver for hostname DNS lookup.
func WithClientDNSResolver(resolver DNSResolver) ClientOption {
	return func(c *Client) {
		c.network.DNSResolver = resolver
		c.applyDialers()
	}
}

// WithClientInspector configures the local developer traffic inspector.
func WithClientInspector(inspector TrafficInspector) ClientOption {
	return func(c *Client) {
		c.defaults.Inspector = inspector
	}
}

// WithClientDoT configures DNS-over-TLS resolution.
func WithClientDoT(endpoint, host string) ClientOption {
	return func(c *Client) {
		c.network.DNSResolver = NewDoTResolver(endpoint, host)
		c.applyDialers()
	}
}

// WithClientDoH configures DNS-over-HTTPS resolution.
func WithClientDoH(endpoint, host string) ClientOption {
	return func(c *Client) {
		c.network.DNSResolver = NewDoHResolver(endpoint, host)
		c.applyDialers()
	}
}

// WithClientChallengeDetector registers a challenge detector (e.g. Cloudflare detection).
func WithClientChallengeDetector(detector ChallengeDetector) ClientOption {
	return func(c *Client) {
		c.defaults.ChallengeDetector = detector
	}
}

// WithClientChallengeSolver configures a custom challenge solver to solve javascript/WAF checks.
func WithClientChallengeSolver(solver ChallengeSolver) ClientOption {
	return func(c *Client) {
		c.defaults.ChallengeSolver = solver
	}
}

// WithClientBeforeRequest registers a hook running prior to outgoing requests.
func WithClientBeforeRequest(hook func(req *http.Request)) ClientOption {
	return func(c *Client) {
		c.defaults.BeforeRequest = append(c.defaults.BeforeRequest, hook)
	}
}

// WithClientAfterResponse registers a hook running after every request completion.
func WithClientAfterResponse(hook func(resp *http.Response, err error)) ClientOption {
	return func(c *Client) {
		c.defaults.AfterResponse = append(c.defaults.AfterResponse, hook) //nolint:bodyclose
	}
}

// WithClientUserAgent sets the default User-Agent request header.
func WithClientUserAgent(ua string) ClientOption {
	return func(c *Client) {
		c.defaults.Headers.Set("User-Agent", ua)
	}
}

// WithClientOrigin sets the default Origin request header.
func WithClientOrigin(origin string) ClientOption {
	return func(c *Client) {
		c.defaults.Headers.Set("Origin", origin)
	}
}

// WithClientBearer sets the default Bearer token Authorization header.
func WithClientBearer(token string) ClientOption {
	return func(c *Client) {
		c.defaults.Headers.Set("Authorization", "Bearer "+token)
	}
}

// WithClientBasicAuth sets the default Basic authentication Authorization header.
func WithClientBasicAuth(username, password string) ClientOption {
	return func(c *Client) {
		c.defaults.Headers.Set(
			"Authorization",
			"Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)),
		)
	}
}

// WithClientCookieJar sets the CookieJar for request cookies.
func WithClientCookieJar(jar http.CookieJar) ClientOption {
	return func(c *Client) {
		if httpClient, ok := c.engine.(*http.Client); ok {
			cloned := *httpClient
			cloned.Jar = jar
			c.engine = &cloned
		}
	}
}

// WithClientConnectionPool configures TCP connection pool limits.
func WithClientConnectionPool(cfg ConnectionPoolConfig) ClientOption {
	return func(c *Client) {
		if transport := c.Transport(); transport != nil {
			transport.MaxIdleConns = generic.Coalesce(cfg.MaxIdleConns, transport.MaxIdleConns)
			transport.MaxIdleConnsPerHost = generic.Coalesce(cfg.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
			transport.MaxConnsPerHost = generic.Coalesce(cfg.MaxConnsPerHost, transport.MaxConnsPerHost)
			transport.IdleConnTimeout = generic.Coalesce(cfg.IdleConnTimeout, transport.IdleConnTimeout)
			transport.ResponseHeaderTimeout = generic.Coalesce(
				cfg.ResponseHeaderTimeout,
				transport.ResponseHeaderTimeout,
			)
		}
	}
}

// WithClientInsecureSkipVerify returns a ClientOption that disables TLS certificate verification
// globally for all requests sent by this client.
//
// Warning: Bypassing verification makes the client vulnerable to man-in-the-middle attacks.
func WithClientInsecureSkipVerify() ClientOption {
	return func(c *Client) {
		if transport := c.Transport(); transport != nil {
			if transport.TLSClientConfig == nil {
				transport.TLSClientConfig = &tls.Config{} //nolint:gosec
			}

			transport.TLSClientConfig.InsecureSkipVerify = true
		}
	}
}

// WithClientTLSFingerprint sets the uTLS BrowserID profile.
func WithClientTLSFingerprint(browser BrowserID) ClientOption {
	return func(c *Client) {
		if browser == BrowserNone {
			return
		}

		c.fingerprint.BrowserID = browser
		c.fingerprint.TLSClientHelloID = nil

		if transport := c.Transport(); transport != nil {
			callback := c.fingerprint.JA4Callback
			tlsConfig := transport.TLSClientConfig
			proxyFn := transport.Proxy
			transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				var proxyURL *url.URL
				if proxyFn != nil {
					proxyURL, _ = proxyFn(&http.Request{URL: &url.URL{Host: addr}})
				}

				return dialTLSWithUTLS(
					ctx,
					network,
					addr,
					browser,
					c.fingerprint.TLSClientHelloID,
					c.network.SourceRotator,
					c.network.DNSResolver,
					callback,
					tlsConfig,
					proxyURL,
				)
			}
		}
	}
}

// WithClientTLSClientHelloSpecProvider configures a custom spec provider for handshakes.
func WithClientTLSClientHelloSpecProvider(provider ClientHelloSpecProvider) ClientOption {
	return func(c *Client) {
		c.fingerprint.TLSClientHelloSpecProvider = provider

		if transport := c.Transport(); transport != nil {
			callback := c.fingerprint.JA4Callback
			tlsConfig := transport.TLSClientConfig
			proxyFn := transport.Proxy
			transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				var proxyURL *url.URL
				if proxyFn != nil {
					proxyURL, _ = proxyFn(&http.Request{URL: &url.URL{Host: addr}})
				}

				return dialTLSWithUTLS(
					ctx,
					network,
					addr,
					c.fingerprint.BrowserID,
					c.fingerprint.TLSClientHelloID,
					c.network.SourceRotator,
					c.network.DNSResolver,
					callback,
					tlsConfig,
					proxyURL,
				)
			}
		}
	}
}

// WithClientJA4Callback sets the callback executed with computed JA4 reports.
func WithClientJA4Callback(fn func(ja4.Report)) ClientOption {
	return func(c *Client) {
		c.fingerprint.JA4Callback = fn
	}
}

// WithClientFragmentation configures TCP packet segmentation properties.
func WithClientFragmentation(cfg FragmentConfig) ClientOption {
	return func(c *Client) {
		c.network.FragmentConfig = &cfg
	}
}

// WithClientHostRewrite sets DNS rewrite rules for SNI vs destination routing.
func WithClientHostRewrite(rules map[string]string) ClientOption {
	return func(c *Client) {
		c.network.HostRewrite = &HostRewriteConfig{Rules: rules}
	}
}

// WithClientProxyIsolatedCookieJar registers a proxy-isolated cookie jar wrapper on transport.
func WithClientProxyIsolatedCookieJar(jar *ProxyIsolatedCookieJar) ClientOption {
	return func(c *Client) {
		c.defaults.HeadersCookieJar = jar
		if httpClient, ok := c.engine.(*http.Client); ok {
			clonedHTTP := *httpClient

			baseTransport := clonedHTTP.Transport
			if baseTransport == nil {
				baseTransport = http.DefaultTransport
			}

			if cjTrans, ok := baseTransport.(*cookieJarTransport); ok {
				baseTransport = cjTrans.next
			}

			clonedHTTP.Transport = &cookieJarTransport{
				next:      baseTransport,
				cookieJar: jar,
			}
			c.engine = &clonedHTTP
		}
	}
}

// WithClientDNSCache registers an in-memory DNS caching resolver wrapper.
func WithClientDNSCache(ttl time.Duration) ClientOption {
	return func(c *Client) {
		c.network.DNSResolver = NewInMemoryDNSCache(ttl, c.network.DNSResolver)
		c.applyDialers()
	}
}

// WithClientHTTP2Settings sets local HTTP/2 connection settings.
func WithClientHTTP2Settings(settings HTTP2Settings) ClientOption {
	return func(c *Client) {
		c.fingerprint.H2Settings = &settings
	}
}

// WithClientH2FramedTransport enables H2 transport wrapper to inject custom SETTINGS/PRIORITY frames.
func WithClientH2FramedTransport(settings HTTP2Settings) ClientOption {
	return func(c *Client) {
		c.fingerprint.H2Settings = &settings
		if transport := c.Transport(); transport != nil {
			if c.fingerprint.H2Configurer != nil {
				t2, err := http2.ConfigureTransports(transport)
				if err == nil && t2 != nil {
					t2.TLSClientConfig = transport.TLSClientConfig
					_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
				}
			}

			framed := NewH2FramedTransport(transport, settings)
			if httpClient, ok := c.engine.(*http.Client); ok {
				httpClient.Transport = framed
			}
		}
	}
}

// WithClientProfileH2Settings extracts H2 transport settings from profiles.
func WithClientProfileH2Settings(s profiles.H2Settings) ClientOption {
	return func(c *Client) {
		settings := H2SettingsFromProfile(s)

		c.fingerprint.H2Settings = &settings
		if transport := c.Transport(); transport != nil {
			if c.fingerprint.H2Configurer != nil {
				t2, err := http2.ConfigureTransports(transport)
				if err == nil && t2 != nil {
					t2.TLSClientConfig = transport.TLSClientConfig
					_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
				}
			}

			framed := NewH2FramedTransport(transport, settings)
			if httpClient, ok := c.engine.(*http.Client); ok {
				httpClient.Transport = framed
			}
		}
	}
}

// WithClientP0fSignature sets p0f stack signatures for TCP/IP emulation.
func WithClientP0fSignature(sig *p0f.Signature) ClientOption {
	return func(c *Client) {
		c.fingerprint.P0fSignature = sig
	}
}

// WithClientSocketController registers a controller intercepting outbound socket descriptors.
func WithClientSocketController(controller SocketController) ClientOption {
	return func(c *Client) {
		c.network.SocketController = controller
		c.applyDialers()
	}
}

// WithClientHTTP2Configurer configures underlying HTTP/2 parameters.
func WithClientHTTP2Configurer(configurer HTTP2Configurer) ClientOption {
	return func(c *Client) {
		c.fingerprint.H2Configurer = configurer
		c.applyDialers()

		if c.fingerprint.H2Settings != nil {
			if transport := c.Transport(); transport != nil {
				t2, err := http2.ConfigureTransports(transport)
				if err == nil && t2 != nil {
					t2.TLSClientConfig = transport.TLSClientConfig
					_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
				}

				framed := NewH2FramedTransport(transport, *c.fingerprint.H2Settings)
				if httpClient, ok := c.engine.(*http.Client); ok {
					httpClient.Transport = framed
				}
			}
		}
	}
}

// WithClientProxyDNS configures DNS resolving via SOCKS5/HTTP Connect proxies.
func WithClientProxyDNS() ClientOption {
	return func(c *Client) {
		c.network.ProxyDNS = true
		c.applyDialers()
	}
}

// WithClientProxy configures proxy server destination.
func WithClientProxy(proxyURL *url.URL) ClientOption {
	return func(c *Client) {
		c.network.ProxyAddr = proxyURL
		if proxyURL != nil {
			c.network.TransportProxy = http.ProxyURL(proxyURL)
		}

		c.applyDialers()
	}
}

// WithClientProxyString configures proxy destination parsing from string formats.
func WithClientProxyString(proxyStr string) ClientOption {
	return func(c *Client) {
		u, err := ParseAutoProxy(proxyStr)
		if err == nil {
			c.network.ProxyAddr = u
			c.network.TransportProxy = http.ProxyURL(u)
			c.applyDialers()
		} else {
			c.network.ProxyAddr = nil
			c.network.TransportProxy = nil
			c.applyDialers()
		}
	}
}

// WithClientHTTP3Settings configures HTTP/3 QUIC connection parameters.
func WithClientHTTP3Settings(settings HTTP3Settings) ClientOption {
	return func(c *Client) {
		c.fingerprint.H3Settings = &settings
	}
}

// WithClientRefererAutomaton enables automatic Referer header tracking.
func WithClientRefererAutomaton(enabled bool) ClientOption {
	return func(c *Client) {
		c.defaults.RefererAutomaton = enabled
	}
}

// WithClientEngine replaces the raw underlying HTTPDoer engine.
func WithClientEngine(engine HTTPDoer) ClientOption {
	return func(c *Client) {
		c.engine = engine
	}
}

// WithClientPipeline configures the client-level default pipeline settings.
func WithClientPipeline(pipe PipelineConfig) ClientOption {
	return func(c *Client) {
		c.defaults.Pipeline = pipe
	}
}

// WithClientResponseValidator sets the default response validator for all requests.
// When a per-request [WithResponseValidator] modifier is also present, both are run sequentially.
func WithClientResponseValidator(fn func(*http.Response) error) ClientOption {
	return func(c *Client) {
		c.defaults.ResponseValidator = fn
	}
}

// WithClientCertificatePin returns a ClientOption that pins the certificate of the given domain
// to the specified public key SHA-256 fingerprint hash globally for all requests sent by this client.
func WithClientCertificatePin(domain, hash string) ClientOption {
	return func(c *Client) {
		if c.fingerprint.CertificatePins == nil {
			c.fingerprint.CertificatePins = make(map[string][]string)
		}

		c.fingerprint.CertificatePins[domain] = append(c.fingerprint.CertificatePins[domain], hash)
	}
}

// WithClientCertificatePins returns a ClientOption that registers a map of domains to their
// respective public key SHA-256 fingerprint hashes globally for all requests sent by this client.
func WithClientCertificatePins(pins map[string][]string) ClientOption {
	return func(c *Client) {
		if c.fingerprint.CertificatePins == nil {
			c.fingerprint.CertificatePins = make(map[string][]string)
		}

		for domain, hashes := range pins {
			c.fingerprint.CertificatePins[domain] = append(c.fingerprint.CertificatePins[domain], hashes...)
		}
	}
}

// WithClientUARotationProfiles sets the list of browser profiles for User-Agent rotation.
func WithClientUARotationProfiles(profiles []BrowserProfile) ClientOption {
	return func(c *Client) {
		c.defaults.UARotationProfiles = profiles
	}
}

// WithMultiReadBody returns a [RequestModifier] that overrides the
// body caching threshold for a single request. Responses smaller
// than threshold are buffered in memory so the body can be read
// multiple times. A value <= 0 disables caching for the request.
func WithMultiReadBody(threshold int64) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk returns a [RequestModifier] that overrides the
// body caching disk-fallback setting for a single request. If disable is true,
// exceeding the memory threshold returns an error ([ErrBufferLimitExceeded]) instead of creating temporary files.
func WithMultiReadDisableDisk(disable bool) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).MultiReadDisableDisk = disable
	}
}
