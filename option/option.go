// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package option provides functional options for customizing an [aoni.Client] configuration.
//
// Options are consumed by [aoni.NewClient] or [aoni.Client.With] to configure global client defaults,
// such as base URLs, timeouts, proxy rotators, TLS fingerprints, and execution pipeline behavior.
//
// Thread Safety:
// All options operate immutably on [aoni.Config] structures, preserving thread safety and concurrent client reuse.
package option

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
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
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ip"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Option is an alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// ============================================================================
// 1. FULL CONFIGURATION BLOCK OVERRIDES
// ============================================================================

// WithConfig returns an [aoni.ClientOption] that replaces the entire client configuration at once.
func WithConfig(cfg aoni.Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Network = cfg.Network.Clone()
		c.Fingerprint = cfg.Fingerprint.Clone()
		c.Defaults = cfg.Defaults.Clone()
		c.Engine = cfg.Engine
	}
}

// WithDefaultsBlock returns an [aoni.ClientOption] replacing only the [aoni.ClientDefaults] configuration layer.
func WithDefaultsBlock(defaults aoni.ClientDefaults) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults = defaults.Clone()
	}
}

// WithNetworkBlock returns an [aoni.ClientOption] replacing only the [aoni.NetworkConfig] configuration layer.
func WithNetworkBlock(network aoni.NetworkConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network = network.Clone()
	}
}

// WithFingerprintBlock returns an [aoni.ClientOption] replacing only the [aoni.FingerprintConfig] configuration layer.
func WithFingerprintBlock(fingerprint aoni.FingerprintConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint = fingerprint.Clone()
	}
}

// ============================================================================
// 2. ENGINE, BASE URL & TRANSPORT OPTIONS
// ============================================================================

// WithEngine returns an [aoni.ClientOption] replacing the underlying [aoni.HTTPDoer] engine (e.g. custom [*http.Client]).
func WithEngine(engine aoni.HTTPDoer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = engine
	}
}

// WithProtocol registers a custom [http.RoundTripper] handler for non-HTTP schemes (e.g., "file", "ftp", "s3").
func WithProtocol(scheme string, handler http.RoundTripper) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Engine.Protocols == nil {
			cfg.Engine.Protocols = make(map[string]http.RoundTripper)
		}

		normScheme := strings.ToLower(strings.TrimSpace(scheme))
		if normScheme != "" && handler != nil {
			cfg.Engine.Protocols[normScheme] = handler
		}
	}
}

// WithBaseURL returns an [aoni.ClientOption] setting the default base URL for resolving relative request paths.
func WithBaseURL(raw string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if raw == "" {
			cfg.Defaults.BaseURL = &url.URL{}
			cfg.Defaults.BaseURLString = ""
			cfg.Defaults.BaseURLTrimmedString = ""

			return
		}

		formatted := raw
		if !strings.HasSuffix(formatted, "/") {
			formatted += "/"
		}

		baseURL, err := url.Parse(formatted)
		if err != nil {
			return
		}

		cfg.Defaults.BaseURL = baseURL
		cfg.Defaults.BaseURLString = baseURL.String()
		cfg.Defaults.BaseURLTrimmedString = strings.TrimSuffix(baseURL.String(), "/")
	}
}

// WithTimeout returns an [aoni.ClientOption] setting the end-to-end request transaction deadline duration.
func WithTimeout(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.Timeout = d
	}
}

// WithRedirectLimit returns an [aoni.ClientOption] setting the maximum number of HTTP redirects followed.
func WithRedirectLimit(max int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.RedirectLimit = max
	}
}

// WithAllowedRedirectDomains returns an [aoni.ClientOption] restricting HTTP redirects to trusted domain patterns.
func WithAllowedRedirectDomains(domains ...string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CheckRedirect = aoni.AllowedDomainsRedirectPolicy(domains...)
	}
}

// WithConnectionPool returns an [aoni.ClientOption] configuring keep-alive connection boundaries on the transport.
func WithConnectionPool(pool aoni.ConnectionPoolConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.ConnectionPool = &pool
	}
}

// WithHTTP2Config returns an [aoni.ClientOption] configuring low-level HTTP/2 connection parameters.
func WithHTTP2Config(cfg aoni.HTTP2Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Engine.HTTP2Config = &cfg
	}
}

// WithHTTP2Configurer returns an [aoni.ClientOption] configuring an [aoni.HTTP2Configurer] interface on the transport.
func WithHTTP2Configurer(configurer aoni.HTTP2Configurer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Configurer = configurer
	}
}

// WithInsecureSkipVerify returns an [aoni.ClientOption] bypassing TLS certificate verification globally on the transport.
//
// Warning:
// Enabling this exposes outgoing connections to man-in-the-middle attacks.
func WithInsecureSkipVerify() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.InsecureSkipVerify = true
	}
}

// ============================================================================
// 3. HEADER, USER-AGENT & AUTH OPTIONS
// ============================================================================

// WithHeader returns an [aoni.ClientOption] adding a default header key-value pair sent with every request.
func WithHeader(key, value string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(key, value)
	}
}

// WithHeaderFunc returns an [aoni.ClientOption] setting a dynamic header evaluated via provider on every request.
func WithHeaderFunc(key string, provider func() string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if key == "" || provider == nil {
			return
		}

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithHeaderFunc(key, provider))
	}
}

// WithHeaders returns an [aoni.ClientOption] merging a map of default headers into the client configuration.
func WithHeaders(headers map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header, len(headers))
		}

		for k, v := range headers {
			cfg.Defaults.Headers.Set(k, v)
		}
	}
}

// WithoutHeaders returns an [aoni.ClientOption] purging all default request headers.
func WithoutHeaders() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Headers = make(http.Header)
	}
}

// WithUserAgent returns an [aoni.ClientOption] overriding the default User-Agent header field.
func WithUserAgent(ua string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("User-Agent", ua)
	}
}

// WithUARotationProfiles returns an [aoni.ClientOption] configuring browser profiles for automatic User-Agent rotation.
func WithUARotationProfiles(profiles []aoni.BrowserProfile) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.UARotationProfiles = profiles
	}
}

// WithOrigin returns an [aoni.ClientOption] setting a default Origin header.
func WithOrigin(origin string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Origin", origin)
	}
}

// WithBearer returns an [aoni.ClientOption] setting a default "Authorization: Bearer <token>" header.
func WithBearer(token string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth returns an [aoni.ClientOption] setting default HTTP Basic Authentication credentials.
func WithBasicAuth(username, password string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		auth := username + ":" + password
		cfg.Defaults.Headers.Set(
			"Authorization",
			"Basic "+base64.StdEncoding.EncodeToString([]byte(auth)),
		)
	}
}

// WithRefererAutomaton returns an [aoni.ClientOption] toggling automatic Referer header tracking across requests.
func WithRefererAutomaton(enabled bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.RefererAutomaton = enabled
	}
}

// ============================================================================
// 4. NETWORK, PROXY & DNS OPTIONS
// ============================================================================

// WithLocalAddr returns an [aoni.ClientOption] binding outgoing TCP connections to a single local IP address.
func WithLocalAddr(addr string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator([]string{addr})
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithLocalAddrPool returns an [aoni.ClientOption] registering a pool of local IP addresses to cycle through.
func WithLocalAddrPool(addrs []string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		rotator, err := ip.NewSourceIPRotator(addrs)
		if err == nil {
			cfg.Network.SourceRotator = rotator
		}
	}
}

// WithProxy returns an [aoni.ClientOption] configuring a proxy server URL.
func WithProxy(proxyURL *url.URL) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyAddr = proxyURL
		if proxyURL != nil {
			cfg.Network.TransportProxy = http.ProxyURL(proxyURL)
		}
	}
}

// WithProxyString returns an [aoni.ClientOption] parsing and setting a proxy URL string.
func WithProxyString(proxyStr string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		u, err := proxy.Parse(proxyStr)
		if err != nil {
			cfg.Network.ProxyAddr = nil
			cfg.Network.TransportProxy = nil
			return
		}

		cfg.Network.ProxyAddr = u
		cfg.Network.TransportProxy = http.ProxyURL(u)
	}
}

// WithProxyDNS returns an [aoni.ClientOption] routing DNS resolutions through SOCKS5 or HTTP CONNECT proxies.
func WithProxyDNS() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ProxyDNS = true
	}
}

// WithDNSResolver returns an [aoni.ClientOption] replacing the default system DNS resolver with an [aoni.DNSResolver].
func WithDNSResolver(resolver aoni.DNSResolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.DNSResolver = resolver
	}
}

// WithHostRewrite returns an [aoni.ClientOption] configuring DNS hostname-to-IP remapping rules.
func WithHostRewrite(rules map[string]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HostRewrite = &aoni.HostRewriteConfig{Rules: rules}
	}
}

// WithHappyEyeballs returns an [aoni.ClientOption] configuring IPv4/IPv6 stagger delay.
func WithHappyEyeballs(delay time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HappyEyeballsDelay = delay
	}
}

// WithSSRFGuard returns an [aoni.ClientOption] enabling SSRF safeguards against private and loopback IP addresses.
func WithSSRFGuard() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SSRFGuard = true
	}
}

// WithTCPDelay returns an [aoni.ClientOption] setting default pre-dial TCP delay jitter bounds.
func WithTCPDelay(min, max time.Duration) aoni.ClientOption {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithTCPDelay(minDelay, maxDelay))
	}
}

// WithFragmentation returns an [aoni.ClientOption] configuring TCP packet fragmentation parameters.
func WithFragmentation(frag fragment.Config) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.FragmentConfig = &frag
	}
}

// WithSocketController returns an [aoni.ClientOption] registering an [aoni.SocketController] socket control hook.
func WithSocketController(controller aoni.SocketController) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.SocketController = controller
	}
}

// ============================================================================
// 5. FINGERPRINT, TLS & H2/H3 EVASION OPTIONS
// ============================================================================

// WithCertCompression enables RFC 8879 TLS Certificate Compression during handshakes
// using the specified algorithms (Brotli, Zstd, Zlib) to reduce packet count and latency.
func WithCertCompression(algos ...cert.CompressionAlgorythm) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if len(algos) == 0 {
			cfg.Fingerprint.CertCompression = []cert.CompressionAlgorythm{
				cert.CompressionBrotli,
				cert.CompressionZstd,
			}

			return
		}

		cfg.Fingerprint.CertCompression = slices.Clone(algos)
	}
}

// WithTLSFingerprint returns an [aoni.ClientOption] selecting a pre-defined [aoni.BrowserID] uTLS ClientHello profile.
func WithTLSFingerprint(browser aoni.BrowserID) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if browser == aoni.BrowserNone {
			return
		}

		cfg.Fingerprint.BrowserID = browser
		cfg.Fingerprint.TLSClientHelloID = nil
	}
}

// WithTLSClientHelloSpecProvider returns an [aoni.ClientOption] setting a dynamic uTLS spec provider.
func WithTLSClientHelloSpecProvider(provider aoni.ClientHelloSpecProvider) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.TLSClientHelloSpecProvider = provider
	}
}

// WithProfileVariant returns an [aoni.ClientOption] configuring TLS fingerprints, HTTP/2 SETTINGS, and browser headers from a [profiles.Variant].
func WithProfileVariant(variant *profiles.Variant, os profiles.OSKey) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if variant == nil {
			return
		}

		aoni.ApplyTLSVariantToConfig(cfg, variant)
		aoni.ApplyHTTPVariantToConfig(cfg, variant, os)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req aoni.Request) {
			aoni.ApplyProfileHeaders(req, variant, os)
		})
	}
}

// WithBrowserProfile returns an [aoni.ClientOption] selecting a pre-defined browser profile for the given operating system.
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

// WithSettings returns an [aoni.ClientOption] setting custom HTTP/2 SETTINGS frame parameters.
func WithSettings(settings h2.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = &settings
	}
}

// WithH2FramedTransport is an alias for [WithSettings].
func WithH2FramedTransport(settings h2.Settings) aoni.ClientOption {
	return WithSettings(settings)
}

// WithProfileH2Settings returns an [aoni.ClientOption] extracting HTTP/2 transport parameters from a [profiles.H2Settings].
func WithProfileH2Settings(s profiles.H2Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = h2.SettingsFromProfile(s)
	}
}

// WithHTTP3Settings returns an [aoni.ClientOption] setting custom HTTP/3 QUIC connection parameters.
func WithHTTP3Settings(settings h3.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H3Settings = &settings
	}
}

// WithP0fSignature returns an [aoni.ClientOption] setting a [p0f.Signature] for OS TCP/IP stack emulation.
func WithP0fSignature(sig *p0f.Signature) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.P0fSignature = sig
	}
}

// WithSessionCache returns an [aoni.ClientOption] assigning an isolated proxy-aware TLS [aoni.SessionCache].
func WithSessionCache(cache aoni.SessionCache) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.SessionCache = cache
	}
}

// WithCertificatePin returns an [aoni.ClientOption] pinning SHA-256 public key hashes globally for a domain.
func WithCertificatePin(domain, hash string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}

		cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], hash)
	}
}

// WithCertificatePins returns an [aoni.ClientOption] registering a map of domain certificate pins globally.
func WithCertificatePins(pins map[string][]string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string, len(pins))
		}

		for domain, hashes := range pins {
			cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], hashes...)
		}
	}
}

// WithPacketPadding returns an [aoni.ClientOption] configuring random packet padding headers to confuse DPI length analysis.
func WithPacketPadding(padding fingerprint.PaddingConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.PacketPadding = &padding
	}
}

// WithJA4Callback returns an [aoni.ClientOption] setting a callback triggered with computed [ja4.Report] signatures.
func WithJA4Callback(fn func(ja4.Report)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.JA4Callback = fn
	}
}

// ============================================================================
// 6. PIPELINE, RESILIENCE, CACHE & BUFFER OPTIONS
// ============================================================================

// WithPipeline returns an [aoni.ClientOption] setting default pipeline configurations.
func WithPipeline(pipe aoni.PipelineConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Pipeline = pipe
	}
}

// WithHedging returns an [aoni.ClientOption] configuring request hedging delay.
func WithHedging(d time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.HedgingDelay = d
	}
}

// WithDynamicHedging returns an [aoni.ClientOption] configuring dynamic RTT-percentile request hedging.
func WithDynamicHedging(config *telemetry.DynamicHedgingConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if config == nil {
			dc := telemetry.DefaultDynamicHedgingConfig()
			cfg.Network.DynamicHedging = &dc
			return
		}

		cfg.Network.DynamicHedging = config
	}
}

// WithMaxResponseSize returns an [aoni.ClientOption] limiting response body consumption in bytes.
func WithMaxResponseSize(size int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MaxResponseSize = size
	}
}

// WithMultiReadBodyThreshold returns an [aoni.ClientOption] setting RAM buffering bounds for replayable reads.
func WithMultiReadBodyThreshold(threshold int64) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadThreshold = threshold
	}
}

// WithMultiReadDisableDisk returns an [aoni.ClientOption] disabling temporary file disk backing on multi-read buffer overflows.
func WithMultiReadDisableDisk(disable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.MultiReadDisableDisk = disable
	}
}

// WithResponseValidator returns an [aoni.ClientOption] setting default response validation functions.
func WithResponseValidator(fn func(*http.Response) error) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ResponseValidator = fn
	}
}

// WithCookieJar returns an [aoni.ClientOption] overriding default cookie storage.
func WithCookieJar(jar http.CookieJar) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CookieJar = jar
	}
}

// WithCookieJanitor returns an [aoni.ClientOption] enabling background cookie purging for [cookie.ProxyIsolatedJar].
func WithCookieJanitor(ctx context.Context, interval time.Duration) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if pJar, ok := cfg.Engine.CookieJar.(*cookie.ProxyIsolatedJar); ok {
			pJar.StartJanitor(ctx, interval)
		}
	}
}

// ============================================================================
// 7. HOOKS, OBSERVABILITY & CHALLENGE OPTIONS
// ============================================================================

// WithBeforeRequest returns an [aoni.ClientOption] registering a hook function executed before dispatching outgoing requests.
func WithBeforeRequest(hook func(req *http.Request)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.BeforeRequest = append(cfg.Defaults.BeforeRequest, hook)
	}
}

// WithAfterResponse returns an [aoni.ClientOption] registering a hook function executed after response completion.
func WithAfterResponse(hook func(resp *http.Response, err error)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.AfterResponse = append(cfg.Defaults.AfterResponse, hook) //nolint:bodyclose
	}
}

// WithModifiers returns an [aoni.ClientOption] registering default request modifiers executed on every request.
func WithModifiers(mods ...aoni.RequestModifier) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mods...)
	}
}

// WithLogger returns an [aoni.ClientOption] assigning a diagnostic [aoni.Logger].
func WithLogger(l aoni.Logger) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Logger = l
	}
}

// WithQueryEncoder returns an [aoni.ClientOption] setting a default query parameters encoder.
func WithQueryEncoder(encoder aoni.QueryEncoder) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.QueryEncoder = encoder
	}
}

// WithBaseResponse returns an [aoni.ClientOption] setting a response provider for structured API response unwrapping.
func WithBaseResponse(provider func() aoni.BaseResponse) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.BaseResponse = provider
	}
}

// WithInspector returns an [aoni.ClientOption] assigning an [aoni.TrafficInspector] diagnostic capturer.
func WithInspector(inspector aoni.TrafficInspector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Inspector = inspector
	}
}

// WithChallengeDetector returns an [aoni.ClientOption] registering an [aoni.ChallengeDetector] WAF challenge detector.
func WithChallengeDetector(detector aoni.ChallengeDetector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ChallengeDetector = detector
	}
}

// WithChallengeSolver returns an [aoni.ClientOption] registering an [aoni.ChallengeSolver] WAF challenge solver.
func WithChallengeSolver(solver aoni.ChallengeSolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ChallengeSolver = solver
	}
}

// WithDecoder returns an [aoni.ClientOption] that registers a custom response decoder for a MIME content type.
func WithDecoder(contentType string, decoder decode.Decoder) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		mediaType, _, _ := strings.Cut(contentType, ";")

		norm := strings.ToLower(strings.TrimSpace(mediaType))
		if norm == "" {
			return
		}

		if cfg.Defaults.Decoders == nil {
			cfg.Defaults.Decoders = make(map[string]aoni.ResponseDecoder)
		}

		if decoder == nil {
			delete(cfg.Defaults.Decoders, norm)
		} else {
			cfg.Defaults.Decoders[norm] = decoder
		}
	}
}

// WithDecoders returns an [aoni.ClientOption] that registers multiple MIME content type decoders.
func WithDecoders(decoders map[string]decode.Decoder) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Decoders == nil {
			cfg.Defaults.Decoders = make(map[string]aoni.ResponseDecoder, len(decoders))
		}

		for contentType, decoder := range decoders {
			mediaType, _, _ := strings.Cut(contentType, ";")

			norm := strings.ToLower(strings.TrimSpace(mediaType))
			if norm != "" {
				if decoder == nil {
					delete(cfg.Defaults.Decoders, norm)
				} else {
					cfg.Defaults.Decoders[norm] = decoder
				}
			}
		}
	}
}
