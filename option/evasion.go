// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"net/http"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	fheader "github.com/lemon4ksan/foundation/net/http/header"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/chrome"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/firefox"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/safari"
	"github.com/lemon4ksan/aoni/internal/profile"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/privacypass"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/aoni/netutil/spki"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
)

// WithChrome configures a production-grade, zero-configuration Google Chrome browser profile.
//
// Automatically synchronizes all layers of the networking stack to match Chromium:
//   - TLS: uTLS Chrome 120+ ClientHello with GREASE extensions, ALPN (h2, http/1.1), and TLS 1.3 key shares.
//   - HTTP/2 & HTTP/3: Chrome SETTINGS frames, WINDOW_UPDATE parameters, and stream priorities.
//   - Headers & Client Hints: Correct Sec-CH-UA, Sec-CH-UA-Mobile, Sec-CH-UA-Platform, and Accept headers.
//   - Security & Privacy: 0-RTT session resumption, Auto-ECH DNS probing, and RFC 8879 cert compression.
//   - Cookies: Automatic [cookie.ProxyIsolatedJar] with CHIPS partitioning.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithChrome(),
//	    option.WithTimeout(15 * time.Second),
//	)
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

		hints := fingerprint.BuildClientHintsForOS(chrome.UserAgentWindows, profiles.Windows)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.Custom(func(req aoni.Request) {
			hints.ApplyHeaders(req.SetHeader)
		}))
	}
}

// WithChromeMobile configures a zero-configuration Chrome Android mobile persona.
//
// Injects mobile User-Agent strings and Sec-CH-UA mobile Client Hints (`?1`).
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithChromeMobile(),
//	)
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

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.Custom(func(req aoni.Request) {
			hints.ApplyHeaders(req.SetHeader)
		}))
	}
}

// WithFirefox configures a zero-configuration Mozilla Firefox browser persona.
//
// Sets Firefox TLS cipher suite ordering, HTTP/2 SETTINGS framing, and Firefox-specific
// Accept headers without Chromium Client Hints.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithFirefox(),
//	)
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

// WithSafari configures a zero-configuration Apple Safari macOS browser persona.
//
// Synchronizes Apple TLS ClientHello signatures, ALPN negotiation, and WebKit HTTP/2 framing.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithSafari(),
//	)
func WithSafari() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		WithProfileVariant(safari.Desktop, profiles.MacOS)(cfg)
		With0RTT(true)(cfg)
		WithCertCompression(cert.CompressionBrotli)(cfg)

		if cfg.Engine.CookieJar == nil {
			cfg.Engine.CookieJar = cookie.NewProxyIsolatedJar()
		}
	}
}

// WithTLSFingerprint selects a pre-defined [aoni.BrowserID] uTLS ClientHello profile.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithTLSFingerprint(aoni.BrowserChrome),
//	)
func WithTLSFingerprint(browser aoni.BrowserID) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if browser == aoni.BrowserNone {
			return
		}

		cfg.Fingerprint.BrowserID = browser
		cfg.Fingerprint.TLSClientHelloID = nil
	}
}

// WithTLSClientHelloID explicitly assigns a low-level [utls.ClientHelloID] preset.
func WithTLSClientHelloID(id utls.ClientHelloID) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.TLSClientHelloID = &id
	}
}

// WithPersonaStruct configures TLS ClientHello ID, HTTP/2 settings, and header order from a [fingerprint.Persona].
func WithPersonaStruct(p fingerprint.Persona) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.TLSClientHelloID = &p.TLSID
		cfg.Fingerprint.H2Settings = &p.H2Settings
		cfg.Fingerprint.HeaderOrder = p.HeaderOrder
		cfg.Fingerprint.P0fSignature = p.P0fSignature

		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(fheader.UserAgent, p.UserAgent)

		if len(p.HeaderOrder) > 0 {
			cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithOrderedHeaders(p.HeaderOrder))
		}
	}
}

// WithTLSClientHelloSpecProvider configures a dynamic callback to generate customized uTLS ClientHello specifications.
func WithTLSClientHelloSpecProvider(provider fingerprint.ClientHelloSpecProvider) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.TLSClientHelloSpecProvider = provider
	}
}

// WithProfileVariant applies a full browser profile variant (TLS, HTTP/2 SETTINGS, headers) for a target OS.
func WithProfileVariant(variant *profiles.Variant, os profiles.OSKey) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if variant == nil {
			return
		}

		profile.ApplyTLSVariantToConfig(cfg, variant)
		profile.ApplyHTTPVariantToConfig(cfg, variant, os)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.Custom(func(req aoni.Request) {
			profile.ApplyProfileHeaders(req, variant, os)
		}))
	}
}

// WithBrowserProfile selects a pre-defined browser profile and tunes it for the specified operating system.
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

// WithP0fSignature configures TCP/IP SYN packet fingerprint spoofing (TTL, Window Size, MSS, WScale, ECN).
//
// Allows mimicking specific host OS TCP stacks (Windows, Linux, iOS, macOS) to defeat p0f L4 passive OS detection.
func WithP0fSignature(sig *p0f.Signature) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.P0fSignature = sig
	}
}

// WithSessionCache assigns an isolated TLS session cache for TLS 1.2/1.3 session resumption.
func WithSessionCache(cache fingerprint.SessionCache) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.SessionCache = cache
	}
}

// With0RTT enables TLS 1.3 and QUIC 0-RTT (Early Data) session resumption (RFC 8446 / RFC 9001).
//
// Sends request payloads inside the initial handshake packet when reconnecting to known servers,
// eliminating one round-trip time.
//
// > [!NOTE]
// > 0-RTT data is vulnerable to network replay attacks. Use primarily for idempotent GET/HEAD requests.
//
// # RFC Compliance
//
// Conforms to RFC 8446 (TLS 1.3) Section 2.3 and RFC 9001 (QUIC TLS).
func With0RTT(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.Enable0RTT = enable
		if enable && cfg.Fingerprint.SessionCache == nil {
			cfg.Fingerprint.SessionCache = proxy.NewProxyAwareSessionCache()
		}
	}
}

// WithCertCompression enables RFC 8879 TLS Certificate Compression during handshakes.
//
// Compresses remote server certificate chains using Brotli or Zstandard to minimize TLS packet count.
//
// # RFC Compliance
//
// Conforms to RFC 8879 (TLS Certificate Compression).
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

// WithCertificatePin pins a SHA-256 certificate public key hash for a target domain.
func WithCertificatePin(domain, hash string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}

		cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], hash)
	}
}

// WithCertificatePins registers multiple domain certificate pins (RFC 7469).
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

// WithSPKIPin registers an RFC 7469 §2.4 Subject Public Key Info (SPKI) SHA-256 fingerprint pin for domain.
func WithSPKIPin(domain, pin string) aoni.ClientOption {
	return WithCertificatePin(domain, spki.NormalizePin(pin))
}

// WithPinnedSPKI registers multiple RFC 7469 §2.4 SPKI SHA-256 fingerprint pins for a domain.
func WithPinnedSPKI(domain string, pins ...string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if len(pins) == 0 {
			return
		}

		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}

		for _, pin := range pins {
			if norm := spki.NormalizePin(pin); norm != "" {
				cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], norm)
			}
		}
	}
}

// WithECHConfig sets raw RFC 9484 TLS 1.3 Encrypted Client Hello (ECH) bytes to encrypt the Server Name Indication (SNI).
//
// # RFC Compliance
//
// Conforms to RFC 9484 / draft-ietf-tls-esni (Encrypted Client Hello).
func WithECHConfig(raw []byte) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.ECHConfigList = slices.Clone(raw)
	}
}

// WithECHConfigBase64 parses and sets base64-encoded ECHConfigList parameters.
func WithECHConfigBase64(rawBase64 string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		decoded, err := fingerprint.ParseECHConfigBase64(rawBase64)
		if err == nil {
			cfg.Fingerprint.ECHConfigList = decoded
		}
	}
}

// WithAutoECH enables automatic DNS HTTPS (Type 65 / RFC 9460) record lookup to discover and apply ECH keys dynamically.
//
// # RFC Compliance
//
// Conforms to RFC 9460 (Service Binding and Parameter Specification for DNS).
func WithAutoECH(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.AutoECH = enable
	}
}

// WithPacketPadding configures pseudo-random HTTP/2-3 frame padding to prevent traffic length analysis attacks.
func WithPacketPadding(padding fingerprint.PaddingConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.PacketPadding = &padding
	}
}

// WithJA4Callback registers a telemetry callback invoked with computed [ja4.Report] signatures for each TLS connection.
func WithJA4Callback(fn func(ja4.Report)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.JA4Callback = fn
	}
}

// WithPersona configures a complete browser persona by common string name ("chrome", "firefox", "safari", "chrome_mobile").
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithPersona("firefox"),
//	)
func WithPersona(name string) aoni.ClientOption {
	switch strings.ToLower(name) {
	case "chrome", "google-chrome", "chromium":
		return WithChrome()
	case "firefox", "ff", "mozilla":
		return WithFirefox()
	case "safari", "apple":
		return WithSafari()
	case "chrome_mobile", "mobile", "android":
		return WithChromeMobile()
	default:
		return WithChrome()
	}
}

// WithPrivacyPass enables automatic RFC 9576 / RFC 9577 Privacy Pass & W3C Private State Tokens challenge solving.
//
// When a remote edge WAF triggers an HTTP 401 with `WWW-Authenticate: PrivateToken`, the client
// requests a cryptographic blind redemption token and automatically retries the request seamlessly.
//
// # RFC Compliance
//
// Conforms to RFC 9576 (Privacy Pass Architecture) and RFC 9577 (Privacy Pass HTTP Authentication Scheme).
func WithPrivacyPass(provider privacypass.TokenProvider) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if provider == nil {
			return
		}

		cfg.Defaults.Pipeline.Challenge = true
		cfg.Defaults.ChallengeDetector = challenge.DetectPrivateTokenChallenge
		cfg.Defaults.ChallengeSolver = challenge.NewPrivateTokenSolver(provider, nil)
	}
}
