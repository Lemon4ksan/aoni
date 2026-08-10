// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"context"
	"net/http"
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
	"github.com/lemon4ksan/aoni/fingerprint/profiles/safari"
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/telemetry"
)

// ============================================================================
// FINGERPRINT, TLS & H2/H3 EVASION OPTIONS
// ============================================================================

// WithChrome applies a production-grade, zero-configuration Chrome profile (DX)
// combining uTLS Chrome 120+, H2/H3 settings, High-Entropy Client Hints, ECH, 0-RTT,
// Certificate Compression, and CHIPS cookie partitioning in one call.
func WithChrome() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		WithProfileVariant(chrome.Desktop, profiles.Windows)(cfg)
		With0RTT(true)(cfg)
		WithAutoECH(true)(cfg)
		WithCertCompression(cert.CompressionBrotli, cert.CompressionZstd)(cfg)
		WithH2ServerPush(true)(cfg)

		if cfg.Engine.CookieJar == nil {
			cfg.Engine.CookieJar = cookie.NewProxyIsolatedJar()
		}

		hints := fingerprint.BuildClientHintsForOS(fingerprint.DefaultUserAgent, profiles.Windows)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req aoni.Request) {
			hints.ApplyHeaders(req.SetHeader)
		})
	}
}

// WithChromeMobile applies a zero-configuration Chrome Android profile (DX) with mobile High-Entropy Client Hints.
func WithChromeMobile() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		WithProfileVariant(chrome.Mobile, profiles.Android)(cfg)
		With0RTT(true)(cfg)
		WithAutoECH(true)(cfg)
		WithCertCompression(cert.CompressionBrotli, cert.CompressionZstd)(cfg)
		WithH2ServerPush(true)(cfg)

		if cfg.Engine.CookieJar == nil {
			cfg.Engine.CookieJar = cookie.NewProxyIsolatedJar()
		}

		hints := fingerprint.BuildClientHintsForOS(chrome.UserAgentAndroid, profiles.Android)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req aoni.Request) {
			hints.ApplyHeaders(req.SetHeader)
		})
	}
}

// WithFirefox applies a zero-configuration Firefox profile (DX) with 0-RTT, ECH, and Cert Compression.
func WithFirefox() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		WithProfileVariant(firefox.Desktop, profiles.Windows)(cfg)
		With0RTT(true)(cfg)
		WithAutoECH(true)(cfg)
		WithCertCompression(cert.CompressionBrotli, cert.CompressionZstd)(cfg)

		if cfg.Engine.CookieJar == nil {
			cfg.Engine.CookieJar = cookie.NewProxyIsolatedJar()
		}
	}
}

// WithSafariDX applies a zero-configuration Safari macOS profile (DX) with 0-RTT, ECH, and Cert Compression.
func WithSafariDX() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		WithProfileVariant(safari.Desktop, profiles.MacOS)(cfg)
		With0RTT(true)(cfg)
		WithAutoECH(true)(cfg)
		WithCertCompression(cert.CompressionBrotli, cert.CompressionZstd)(cfg)

		if cfg.Engine.CookieJar == nil {
			cfg.Engine.CookieJar = cookie.NewProxyIsolatedJar()
		}
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

// With0RTT enables TLS 1.3 and QUIC 0-RTT (Early Data) session resumption (RFC 9001 / RFC 8446)
// to send initial request payloads in the first packet, reducing connection setup latency to zero RTTs.
//
// Security Note:
// 0-RTT data can be subject to network replay attacks. Use primarily for idempotent GET/HEAD requests.
func With0RTT(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.Enable0RTT = enable
		if enable && cfg.Fingerprint.SessionCache == nil {
			cfg.Fingerprint.SessionCache = proxy.NewProxyAwareSessionCache()
		}
	}
}

// WithCertCompression enables RFC 8879 TLS Certificate Compression during handshakes
// using the specified algorithms to reduce packet count and latency.
func WithCertCompression(algos ...cert.CompressionAlgorithm) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if len(algos) == 0 {
			cfg.Fingerprint.CertCompression = []cert.CompressionAlgorithm{
				cert.CompressionBrotli,
				cert.CompressionZstd,
			}

			return
		}

		cfg.Fingerprint.CertCompression = slices.Clone(algos)
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

// WithECHConfig configures raw RFC 9484 TLS 1.3 Encrypted Client Hello (ECH) bytes to encrypt SNI.
func WithECHConfig(raw []byte) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.ECHConfigList = slices.Clone(raw)
	}
}

// WithECHConfigBase64 configures base64-encoded ECHConfigList parameters to encrypt SNI.
func WithECHConfigBase64(rawBase64 string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		decoded, err := fingerprint.ParseECHConfigBase64(rawBase64)
		if err == nil {
			cfg.Fingerprint.ECHConfigList = decoded
		}
	}
}

// WithAutoECH enables automatic DNS HTTPS (Type 65) record resolution to retrieve ECHConfig keys.
func WithAutoECH(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.AutoECH = enable
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
// PIPELINE, RESILIENCE, CACHE & BUFFER OPTIONS
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

// WithCookieIndices enables selective cookie-based response caching, hashing only specified
// cookie names (e.g., "theme", "lang") into the cache key to maximize hit rates for static pages.
func WithCookieIndices(cookieNames ...string) aoni.ClientOption {
	return func(c *aoni.Config) {
		if c.Defaults.Pipeline.Cache == nil {
			c.Defaults.Pipeline.Cache = &aoni.CacheConfig{}
		}

		c.Defaults.Pipeline.Cache.CookieIndices = slices.Clone(cookieNames)
	}
}

// WithDuplicateRequestGuard enables ring-buffer duplicate request detection,
// triggering a diagnostic alert if the same URL is fetched within the window (e.g. 10s).
func WithDuplicateRequestGuard(window time.Duration, logger aoni.Logger) aoni.ClientOption {
	if window <= 0 {
		window = 10 * time.Second
	}

	guard := telemetry.NewDuplicateRequestGuard(128, window, func(method, rawURL string, elapsed time.Duration) {
		if logger != nil {
			logger.Warn("aoni telemetry: potential duplicate request loop detected",
				"method", method,
				"url", rawURL,
				"elapsed", elapsed,
			)
		}
	})

	return func(cfg *aoni.Config) {
		cfg.Defaults.BeforeRequest = append(cfg.Defaults.BeforeRequest, func(req *http.Request) {
			guard.CheckAndRecord(req.Method, req.URL.String())
		})
	}
}

// ============================================================================
// HOOKS, OBSERVABILITY & CHALLENGE OPTIONS
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

// WithOSPowerManagement enables OS sleep and resume monitoring, automatically purging
// stale zombie sockets and connection pools when the system wakes up from sleep.
func WithOSPowerManagement(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.EnablePowerManagement = enable
	}
}

// WithConnFilter registers custom stream codec filters evaluated during socket dialing.
func WithConnFilter(filters ...aoni.ConnFilter) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ConnFilters = append(cfg.Network.ConnFilters, filters...)
	}
}

// WithLocale returns an [aoni.ClientOption] that configures the Accept-Language header
// for localization matching target proxy countries (e.g., "fr-FR,fr;q=0.9,en-US;q=0.8").
func WithLocale(locale string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set("Accept-Language", locale)
	}
}

// ExperimentalFlag defines bitwise feature flags for opt-in hardware and OS optimizations.
type ExperimentalFlag = aoni.ExperimentalFlag

const (
	// ExpKernelBypass enables io_uring / RIO kernel ring buffer I/O.
	ExpKernelBypass = aoni.ExpKernelBypass

	// ExpSIMD enables AVX2 / AVX-512 hardware vector acceleration.
	ExpSIMD = aoni.ExpSIMD

	// ExpZeroCopy enables Linux splice / sendfile zero-copy socket transfers.
	ExpZeroCopy = aoni.ExpZeroCopy

	// ExpRIO enables Windows Winsock Registered I/O extensions.
	ExpRIO = aoni.ExpRIO

	// ExpTCPFastOpen enables 0-RTT TCP FastOpen socket connection tuning (RFC 7413).
	ExpTCPFastOpen = aoni.ExpTCPFastOpen

	// ExpBusyPoll enables low-latency kernel socket driver polling (SO_BUSY_POLL).
	ExpBusyPoll = aoni.ExpBusyPoll
)

// WithExperimental enables one or more experimental hardware/OS features via bitwise flags or list.
//
// Backward Compatibility Guarantee:
// Gating experimental features under a single option protects the public API contract,
// allowing future experimental flags to be added, renamed, or retired without breaking changes.
func WithExperimental(flags ...ExperimentalFlag) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		for _, f := range flags {
			cfg.Network.ExperimentalFlags |= f
		}
	}
}

// WithCPUAffinity locks worker OS threads to designated CPU core indices.
func WithCPUAffinity(cores ...int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.CPUAffinityCores = append(cfg.Network.CPUAffinityCores, cores...)
	}
}
