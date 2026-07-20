// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni/h3"
	"github.com/lemon4ksan/aoni/ja4"
	"github.com/lemon4ksan/aoni/profiles"
	"github.com/lemon4ksan/aoni/profiles/chrome"
	"github.com/lemon4ksan/aoni/profiles/firefox"
)

func withBaseURL(raw string) ClientOption {
	return func(cfg *Config) {
		if raw == "" {
			cfg.Defaults.BaseURL = &url.URL{}
			return
		}
		if !strings.HasSuffix(raw, "/") {
			raw += "/"
		}
		u, err := url.Parse(raw)
		if err == nil {
			cfg.Defaults.BaseURL = u
		}
	}
}

func withTimeout(d time.Duration) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.Timeout = d
	}
}

func withHeaders(headers map[string]string) ClientOption {
	return func(cfg *Config) {
		for k, v := range headers {
			cfg.Defaults.Headers.Set(k, v)
		}
	}
}

func withHeader(key, value string) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.Headers.Set(key, value)
	}
}

func withTLSFingerprint(browser BrowserID) ClientOption {
	return func(cfg *Config) {
		if browser == BrowserNone {
			return
		}
		cfg.Fingerprint.BrowserID = browser
		cfg.Fingerprint.TLSClientHelloID = nil
	}
}

func withRedirectLimit(max int) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.RedirectLimit = max
	}
}

func withHedging(d time.Duration) ClientOption {
	return func(cfg *Config) {
		cfg.Network.HedgingDelay = d
	}
}

func withBeforeRequest(hook func(req *http.Request)) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.BeforeRequest = append(cfg.Defaults.BeforeRequest, hook)
	}
}

func withAfterResponse(hook func(resp *http.Response, err error)) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.AfterResponse = append(cfg.Defaults.AfterResponse, hook) //nolint:bodyclose
	}
}

func withConnectionPool(pool ConnectionPoolConfig) ClientOption {
	return func(cfg *Config) {
		cfg.Engine.ConnectionPool = &pool
	}
}

func withJA4Callback(fn func(ja4.Report)) ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.JA4Callback = fn
	}
}

func withQueryEncoder(encoder QueryEncoder) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.QueryEncoder = encoder
	}
}

func withBrowserProfile(browser BrowserID, os profiles.OSKey) ClientOption {
	var variant *profiles.Variant
	switch browser {
	case BrowserFirefox:
		variant = generic.Ternary(os.IsMobile(), firefox.Mobile, firefox.Desktop)
	default:
		variant = generic.Ternary(os.IsMobile(), chrome.Mobile, chrome.Desktop)
	}

	return withProfileVariant(variant, os)
}

func withProfileVariant(variant *profiles.Variant, os profiles.OSKey) ClientOption {
	return func(cfg *Config) {
		if variant == nil {
			return
		}
		ApplyTLSVariantToConfig(cfg, variant)
		ApplyHTTPVariantToConfig(cfg, variant, os)
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req *http.Request) {
			ApplyProfileHeaders(req, variant, os)
		})
	}
}

func withTCPDelay(min, max time.Duration) ClientOption {
	if min > max {
		min, max = max, min
	}
	return func(cfg *Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, func(req *http.Request) {
			getOrInitRequestConfig(req).TCPDelay = TCPDelayRange{Min: min, Max: max}
		})
	}
}

func withInsecureSkipVerify() ClientOption {
	return func(cfg *Config) {
		cfg.Engine.InsecureSkipVerify = true
	}
}

func withResponseValidator(fn func(*http.Response) error) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.ResponseValidator = fn
	}
}

func withBaseResponse(provider func() BaseResponse) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.BaseResponse = provider
	}
}

func withChallengeSolver(solver ChallengeSolver) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.ChallengeSolver = solver
	}
}

func withChallengeDetector(detector ChallengeDetector) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.ChallengeDetector = detector
	}
}

func withMaxResponseSize(size int64) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.MaxResponseSize = size
	}
}

func withRefererAutomaton(enabled bool) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.RefererAutomaton = enabled
	}
}

func withHappyEyeballs(delay time.Duration) ClientOption {
	return func(cfg *Config) {
		cfg.Network.HappyEyeballsDelay = delay
	}
}

func withSSRFGuard() ClientOption {
	return func(cfg *Config) {
		cfg.Network.SSRFGuard = true
	}
}

func withMultiReadDisableDisk(disable bool) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.MultiReadDisableDisk = disable
	}
}

func withUARotationProfiles(profs []BrowserProfile) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.UARotationProfiles = profs
	}
}

func withPipeline(pipe PipelineConfig) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.Pipeline = pipe
	}
}

func withInspector(inspector TrafficInspector) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.Inspector = inspector
	}
}

func withMultiReadBody(threshold int64) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.MultiReadThreshold = threshold
	}
}

func withHTTP3Settings(settings h3.Settings) ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.H3Settings = &settings
	}
}

func withCertificatePin(domain, hash string) ClientOption {
	return func(cfg *Config) {
		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}
		cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], hash)
	}
}

func withCertificatePins(pins map[string][]string) ClientOption {
	return func(cfg *Config) {
		if cfg.Fingerprint.CertificatePins == nil {
			cfg.Fingerprint.CertificatePins = make(map[string][]string)
		}
		for domain, hashes := range pins {
			cfg.Fingerprint.CertificatePins[domain] = append(cfg.Fingerprint.CertificatePins[domain], hashes...)
		}
	}
}

func withTLSClientHelloSpecProvider(provider ClientHelloSpecProvider) ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.TLSClientHelloSpecProvider = provider
	}
}

func withProxy(proxyURL *url.URL) ClientOption {
	return func(cfg *Config) {
		cfg.Network.ProxyAddr = proxyURL
		if proxyURL != nil {
			cfg.Network.TransportProxy = http.ProxyURL(proxyURL)
		}
	}
}

func withSocketController(controller SocketController) ClientOption {
	return func(cfg *Config) {
		cfg.Network.SocketController = controller
	}
}

func withHTTP2Configurer(configurer HTTP2Configurer) ClientOption {
	return func(cfg *Config) {
		cfg.Fingerprint.H2Configurer = configurer
	}
}

func withLogger(l Logger) ClientOption {
	return func(cfg *Config) {
		cfg.Defaults.Logger = l
	}
}






