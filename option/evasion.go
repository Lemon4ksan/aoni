// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"net/http"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
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
	"github.com/lemon4ksan/aoni/netutil/hpkp"
	"github.com/lemon4ksan/aoni/netutil/proxy"
)

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

		hints := fingerprint.BuildClientHintsForOS(chrome.UserAgentWindows, profiles.Windows)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.Custom(func(req aoni.Request) {
			hints.ApplyHeaders(req.SetHeader)
		}))
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

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.Custom(func(req aoni.Request) {
			hints.ApplyHeaders(req.SetHeader)
		}))
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

// WithSafari applies a zero-configuration Safari macOS profile (DX) with 0-RTT and Brotli compression.
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

// WithTLSClientHelloID returns an [aoni.ClientOption] setting a specific uTLS ClientHelloID preset.
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

		cfg.Defaults.Headers.Set("User-Agent", p.UserAgent)

		if len(p.HeaderOrder) > 0 {
			cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.WithOrderedHeaders(p.HeaderOrder))
		}
	}
}

// WithTLSClientHelloSpecProvider returns an [aoni.ClientOption] setting a dynamic uTLS spec provider.
func WithTLSClientHelloSpecProvider(provider fingerprint.ClientHelloSpecProvider) aoni.ClientOption {
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

		profile.ApplyTLSVariantToConfig(cfg, variant)
		profile.ApplyHTTPVariantToConfig(cfg, variant, os)

		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mod.Custom(func(req aoni.Request) {
			profile.ApplyProfileHeaders(req, variant, os)
		}))
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

// WithP0fSignature returns an [aoni.ClientOption] setting a [p0f.Signature] for OS TCP/IP stack emulation.
func WithP0fSignature(sig *p0f.Signature) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.P0fSignature = sig
	}
}

// WithSessionCache returns an [aoni.ClientOption] assigning an isolated proxy-aware TLS [fingerprint.SessionCache].
func WithSessionCache(cache fingerprint.SessionCache) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.SessionCache = cache
	}
}

// With0RTT enables TLS 1.3 and QUIC 0-RTT (Early Data) session resumption (RFC 9001 / RFC 8446 / RFC 9846)
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

// WithCertificatePins returns an [aoni.ClientOption] registering a map of domain certificate pins globally (RFC 7469).
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

// WithSPKIPin returns an [aoni.ClientOption] pinning an SPKI SHA-256 fingerprint hash globally for domain (RFC 7469 §2.4).
func WithSPKIPin(domain, pin string) aoni.ClientOption {
	return WithCertificatePin(domain, pin)
}

// WithHPKPPolicy returns an [aoni.ClientOption] registering all pins from an RFC 7469 [hpkp.Policy] for domain.
func WithHPKPPolicy(domain string, policy *hpkp.Policy) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if policy == nil || len(policy.Pins) == 0 {
			return
		}

		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}

		for _, pin := range policy.Pins {
			cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], pin.Fingerprint)
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

// WithPersona configures a browser persona by name (e.g. "chrome", "firefox", "safari", "chrome_mobile").
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
