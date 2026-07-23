// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package option provides functional options for configuring an [aoni.Client].
//
// Options are passed to [aoni.NewClient] or [aoni.Client.With] to configure global client
// defaults, such as base URLs, request timeouts, proxy rotators, TLS fingerprints,
// and pipeline execution flags.
//
// All options operate immutably on [aoni.Config] structs, ensuring that thread safety
// and concurrent client reusability are preserved.
package option

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/chrome"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/firefox"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/ip"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Option is a type alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// WithConfig replaces the entire client configuration at once.
func WithConfig(cfg aoni.Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Network = cfg.Network.Clone()
		c.Fingerprint = cfg.Fingerprint.Clone()
		c.Defaults = cfg.Defaults.Clone()
		c.Engine = cfg.Engine
	}
}

// WithDefaultsBlock replaces only the default parameters block.
func WithDefaultsBlock(defaults aoni.ClientDefaults) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults = defaults.Clone()
	}
}

// WithNetworkBlock replaces only the network layer block.
func WithNetworkBlock(network aoni.NetworkConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network = network.Clone()
	}
}

// WithFingerprintBlock replaces only the fingerprint layer block.
func WithFingerprintBlock(fingerprint aoni.FingerprintConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint = fingerprint.Clone()
	}
}

// WithLogger sets the diagnostic logger for the client.
func WithLogger(l aoni.Logger) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Logger = l
	}
}

// WithModifiers registers request modifiers that run on every request
// before the middleware chain.
func WithModifiers(mods ...aoni.RequestModifier) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mods...)
	}
}

// WithBaseResponse sets the response provider for structured API unwrapping.
func WithBaseResponse(provider func() aoni.BaseResponse) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.BaseResponse = provider
	}
}

// WithQueryEncoder configures the default query parameters encoder for the client.
func WithQueryEncoder(encoder aoni.QueryEncoder) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.QueryEncoder = encoder
	}
}

// WithHTTP2Config configures the low-level HTTP/2 connection parameters.
func WithHTTP2Config(cfg aoni.HTTP2Config) aoni.ClientOption {
	// For consistency with http3 we leave this option in the core module
	return func(c *aoni.Config) {
		c.Engine.HTTP2Config = &cfg
	}
}

// WithBaseURL configures the base URL for resolving relative request paths.
func WithBaseURL(raw string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if raw == "" {
			cfg.Defaults.BaseURL = &url.URL{}
			return
		}

		if !strings.HasSuffix(raw, "/") {
			raw += "/"
		}

		baseURL, err := url.Parse(raw)
		if err == nil {
			cfg.Defaults.BaseURL = baseURL
		}
	}
}

// WithHeader adds a default HTTP header sent with every request.
func WithHeader(key, value string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(key, value)
	}
}

// WithHeaders merges the provided map of headers into the default request headers.
func WithHeaders(headers map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		for k, v := range headers {
			cfg.Defaults.Headers.Set(k, v)
		}
	}
}

// WithoutHeaders removes all default request headers.
func WithoutHeaders() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers = make(http.Header)
	}
}

// WithTimeout configures the request deadline.
func WithTimeout(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.Timeout = d
	}
}

// WithAllowedRedirectDomains restricts HTTP redirects to a specified list of trusted domain names.
func WithAllowedRedirectDomains(domains ...string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CheckRedirect = aoni.AllowedDomainsRedirectPolicy(domains...)
	}
}

// WithProfileVariant configures the TLS fingerprint, HTTP/2 setting frames,
// and default browser headers to match the provided custom browser profile variant.
func WithProfileVariant(variant *profiles.Variant, os profiles.OSKey) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if variant == nil {
			return
		}

		aoni.ApplyTLSVariantToConfig(cfg, variant)
		aoni.ApplyHTTPVariantToConfig(cfg, variant, os)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req *http.Request) {
			aoni.ApplyProfileHeaders(req, variant, os)
		})
	}
}

// WithBrowserProfile configures the TLS fingerprint, HTTP/2 setting frames,
// and default browser headers to match the selected browser profile.
func WithBrowserProfile(browser aoni.BrowserID, os profiles.OSKey) aoni.ClientOption {
	var variant *profiles.Variant

	switch browser {
	case aoni.BrowserFirefox:
		variant = generic.Ternary(os.IsMobile(), firefox.Mobile, firefox.Desktop)
	default:
		variant = generic.Ternary(os.IsMobile(), chrome.Mobile, chrome.Desktop)
	}

	return WithProfileVariant(variant, os)
}

// WithTCPDelay sets the default TCP connection delay range for all requests.
func WithTCPDelay(min, max time.Duration) aoni.ClientOption {
	if min > max {
		min, max = max, min
	}

	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithTCPDelay(min, max))
	}
}

// WithRedirectLimit sets the maximum redirect count.
func WithRedirectLimit(max int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.RedirectLimit = max
	}
}

// WithLocalAddr configures the local IP address to bind outgoing connections to.
func WithLocalAddr(addr string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator([]string{addr})
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithHedging configures the request hedging delay.
func WithHedging(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HedgingDelay = d
	}
}

// WithDynamicHedging configures dynamic request hedging.
func WithDynamicHedging(config *telemetry.DynamicHedgingConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if config == nil {
			dc := telemetry.DefaultDynamicHedgingConfig()
			cfg.Network.DynamicHedging = &dc
		} else {
			cfg.Network.DynamicHedging = config
		}
	}
}

// WithSessionCache enables the TLS session ticket cache.
func WithSessionCache(cache aoni.SessionCache) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.SessionCache = cache
	}
}

// WithPacketPadding configures packet padding to obscure segments against DPI.
func WithPacketPadding(padding fingerprint.PaddingConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.PacketPadding = &padding
	}
}

// WithMaxResponseSize limits the maximum bytes allowed in response bodies.
func WithMaxResponseSize(size int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MaxResponseSize = size
	}
}

// WithSSRFGuard enables SSRF protection by blocking requests resolving to private/loopback IPs.
func WithSSRFGuard() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SSRFGuard = true
	}
}

// WithHappyEyeballs configures the staggered Happy Eyeballs delay.
func WithHappyEyeballs(delay time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HappyEyeballsDelay = delay
	}
}

// WithMultiReadBody sets the multi-read threshold in bytes.
func WithMultiReadBody(threshold int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk disables disk fallbacks when multi-read cache limit is reached.
func WithMultiReadDisableDisk(disable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadDisableDisk = disable
	}
}

// WithLocalAddrPool registers a list of local IP addresses to cycle through.
func WithLocalAddrPool(addrs []string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator(addrs)
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithDNSResolver sets the resolver for hostname DNS lookup.
func WithDNSResolver(resolver aoni.DNSResolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.DNSResolver = resolver
	}
}

// WithInspector configures the local developer traffic inspector.
func WithInspector(inspector aoni.TrafficInspector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Inspector = inspector
	}
}

// WithChallengeDetector registers a challenge detector (e.g. Cloudflare detection).
func WithChallengeDetector(detector aoni.ChallengeDetector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ChallengeDetector = detector
	}
}

// WithChallengeSolver configures a custom challenge solver to solve javascript/WAF checks.
func WithChallengeSolver(solver aoni.ChallengeSolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ChallengeSolver = solver
	}
}

// WithBeforeRequest registers a hook running prior to outgoing requests.
func WithBeforeRequest(hook func(req *http.Request)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.BeforeRequest = append(cfg.Defaults.BeforeRequest, hook)
	}
}

// WithAfterResponse registers a hook running after every request completion.
func WithAfterResponse(hook func(resp *http.Response, err error)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.AfterResponse = append(cfg.Defaults.AfterResponse, hook) //nolint:bodyclose
	}
}

// WithUserAgent sets the default User-Agent request header.
func WithUserAgent(ua string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers.Set("User-Agent", ua)
	}
}

// WithOrigin sets the default Origin request header.
func WithOrigin(origin string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers.Set("Origin", origin)
	}
}

// WithBearer sets the default Bearer token Authorization header.
func WithBearer(token string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers.Set("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth sets the default Basic authentication Authorization header.
func WithBasicAuth(username, password string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers.Set(
			"Authorization",
			"Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)),
		)
	}
}

// WithCookieJar sets the CookieJar for request cookies.
func WithCookieJar(jar http.CookieJar) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CookieJar = jar
	}
}

// WithCookieJanitor enables automatic periodic background purging of expired cookies
// for the client's cookie jar at the specified interval.
func WithCookieJanitor(ctx context.Context, interval time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if pJar, ok := cfg.Engine.CookieJar.(*cookie.ProxyIsolatedJar); ok {
			pJar.StartJanitor(ctx, interval)
		}
	}
}

// WithConnectionPool configures TCP connection pool limits.
func WithConnectionPool(pool aoni.ConnectionPoolConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.ConnectionPool = &pool
	}
}

// WithInsecureSkipVerify disables TLS certificate verification globally.
func WithInsecureSkipVerify() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.InsecureSkipVerify = true
	}
}

// WithTLSFingerprint sets the uTLS BrowserID profile.
func WithTLSFingerprint(browser aoni.BrowserID) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if browser == aoni.BrowserNone {
			return
		}

		cfg.Fingerprint.BrowserID = browser
		cfg.Fingerprint.TLSClientHelloID = nil
	}
}

// WithTLSClientHelloSpecProvider configures a custom spec provider for handshakes.
func WithTLSClientHelloSpecProvider(provider aoni.ClientHelloSpecProvider) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.TLSClientHelloSpecProvider = provider
	}
}

// WithJA4Callback sets the callback executed with computed JA4 reports.
func WithJA4Callback(fn func(ja4.Report)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.JA4Callback = fn
	}
}

// WithFragmentation configures TCP packet segmentation properties.
func WithFragmentation(frag aoni.FragmentConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.FragmentConfig = &frag
	}
}

// WithHostRewrite sets DNS rewrite rules for SNI vs destination routing.
func WithHostRewrite(rules map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HostRewrite = &aoni.HostRewriteConfig{Rules: rules}
	}
}

// WithSettings sets local HTTP/2 connection settings.
func WithSettings(settings h2.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = &settings
	}
}

// WithH2FramedTransport enables H2 transport wrapper to inject custom SETTINGS/PRIORITY frames.
func WithH2FramedTransport(settings h2.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = &settings
	}
}

// WithProfileH2Settings extracts H2 transport settings from profiles.
func WithProfileH2Settings(s profiles.H2Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = h2.SettingsFromProfile(s)
	}
}

// WithP0fSignature sets p0f stack signatures for TCP/IP emulation.
func WithP0fSignature(sig *p0f.Signature) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.P0fSignature = sig
	}
}

// WithSocketController registers a controller intercepting outbound socket descriptors.
func WithSocketController(controller aoni.SocketController) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SocketController = controller
	}
}

// WithHTTP2Configurer configures underlying HTTP/2 parameters.
func WithHTTP2Configurer(configurer aoni.HTTP2Configurer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Configurer = configurer
	}
}

// WithProxyDNS configures DNS resolving via SOCKS5/HTTP Connect proxies.
func WithProxyDNS() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyDNS = true
	}
}

// WithProxy configures proxy server destination.
func WithProxy(proxyURL *url.URL) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyAddr = proxyURL
		if proxyURL != nil {
			cfg.Network.TransportProxy = http.ProxyURL(proxyURL)
		}
	}
}

// WithProxyString configures proxy destination parsing from string formats.
func WithProxyString(proxyStr string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		u, err := proxy.Parse(proxyStr)
		if err == nil {
			cfg.Network.ProxyAddr = u
			cfg.Network.TransportProxy = http.ProxyURL(u)
		} else {
			cfg.Network.ProxyAddr = nil
			cfg.Network.TransportProxy = nil
		}
	}
}

// WithHTTP3Settings configures HTTP/3 QUIC connection parameters.
func WithHTTP3Settings(settings h3.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H3Settings = &settings
	}
}

// WithRefererAutomaton enables automatic Referer header tracking.
func WithRefererAutomaton(enabled bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.RefererAutomaton = enabled
	}
}

// WithEngine replaces the raw underlying HTTPDoer engine.
func WithEngine(engine aoni.HTTPDoer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = engine
	}
}

// WithPipeline configures the client-level default pipeline settings.
func WithPipeline(pipe aoni.PipelineConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Pipeline = pipe
	}
}

// WithResponseValidator sets the default response validator for all requests.
func WithResponseValidator(fn func(*http.Response) error) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ResponseValidator = fn
	}
}

// WithCertificatePin returns a ClientOption that pins the certificate of the given domain
// to the specified public key SHA-256 fingerprint hash globally for all requests sent by this client.
func WithCertificatePin(domain, hash string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}

		cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], hash)
	}
}

// WithCertificatePins returns a ClientOption that registers a map of domains to their
// respective public key SHA-256 fingerprint hashes globally for all requests sent by this client.
func WithCertificatePins(pins map[string][]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}

		for domain, hashes := range pins {
			cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], hashes...)
		}
	}
}

// WithUARotationProfiles sets the list of browser profiles for User-Agent rotation.
func WithUARotationProfiles(profiles []aoni.BrowserProfile) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.UARotationProfiles = profiles
	}
}
