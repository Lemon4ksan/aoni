// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"net/http"
	"strings"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/resiliency"
	"github.com/lemon4ksan/aoni/telemetry"
)

// WithBeforeRequest registers a diagnostic or tracing hook executed immediately before dispatching an HTTP request.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithBeforeRequest(func(req *http.Request) {
//	        log.Printf("--> %s %s", req.Method, req.URL.String())
//	    }),
//	)
func WithBeforeRequest(hook func(req *http.Request)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.BeforeRequest = append(cfg.Defaults.BeforeRequest, hook)
	}
}

// WithAfterResponse registers a diagnostic or telemetry hook executed immediately after response reception.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithAfterResponse(func(resp *http.Response, err error) {
//	        if err != nil {
//	            log.Printf("<-- ERROR: %v", err)
//	            return
//	        }
//	        log.Printf("<-- %d %s", resp.StatusCode, resp.Status)
//	    }),
//	)
func WithAfterResponse(hook func(resp *http.Response, err error)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.AfterResponse = append(cfg.Defaults.AfterResponse, hook) //nolint:bodyclose
	}
}

// WithModifiers registers default [aoni.RequestModifier] functions executed sequentially on every outbound request.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithModifiers(
//	        mod.WithHeader("X-Client", "aoni-v1"),
//	        mod.WithTimeout(5 * time.Second),
//	    ),
//	)
func WithModifiers(mods ...aoni.RequestModifier) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.DefaultMods = append(cfg.Defaults.DefaultMods, mods...)
	}
}

// WithLogger assigns a structured diagnostic [telemetry.Logger] for pipeline and transport event tracing.
func WithLogger(l core.Logger) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Logger = l
	}
}

// WithQueryEncoder configures a custom serializer for encoding struct or map values into URL query strings.
func WithQueryEncoder(encoder aoni.QueryEncoder) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.QueryEncoder = encoder
	}
}

// WithBaseResponse sets a factory for unwrapping structured envelope responses (e.g. `{ "data": T, "error": null }`).
func WithBaseResponse(provider func() aoni.BaseResponse) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.BaseResponse = provider
	}
}

// WithInspector assigns a diagnostic [telemetry.TrafficInspector] for capturing network HAR records or live inspection.
func WithInspector(inspector telemetry.TrafficInspector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Inspector = inspector
	}
}

// WithChallengeDetector registers a custom detector to identify bot challenge and WAF interception responses.
func WithChallengeDetector(detector resiliency.ChallengeDetector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ChallengeDetector = detector
	}
}

// WithChallengeSolver registers an automated solver for completing WAF challenges (e.g. Cloudflare Turnstile, Privacy Pass).
func WithChallengeSolver(solver resiliency.ChallengeSolver) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ChallengeSolver = solver
	}
}

// WithDecoder registers a custom response body unmarshaler for a specific MIME Content-Type.
//
// Overrides the built-in decoders for JSON, XML, or Protobuf.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithDecoder("application/x-msgpack", msgpackDecoder),
//	)
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

// WithDecoders registers a map of custom response body unmarshalers for multiple MIME Content-Types.
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

// WithOSPowerManagement enables OS sleep and wake event monitoring.
//
// Proactively flushes stale TCP keep-alive sockets and pool entries when the host OS wakes up from sleep.
func WithOSPowerManagement(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.EnablePowerManagement = enable
	}
}

// WithConnFilter registers stream filters evaluated dynamically during socket dialing.
func WithConnFilter(filters ...aoni.ConnFilter) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.ConnFilters = append(cfg.Network.ConnFilters, filters...)
	}
}

// WithLocale sets the default Accept-Language header matching target country locales.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithLocale("de-DE,de;q=0.9,en-US;q=0.8"),
//	)
func WithLocale(locale string) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		cfg.Defaults.Headers.Set(fheader.AcceptLanguage, locale)
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

// WithExperimental enables one or more experimental hardware or OS acceleration flags.
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithExperimental(option.ExpSIMD, option.ExpTCPFastOpen),
//	)
func WithExperimental(flags ...ExperimentalFlag) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		for _, f := range flags {
			cfg.Network.ExperimentalFlags |= f
		}
	}
}

// WithCPUAffinity locks client worker OS threads to designated CPU core indices.
func WithCPUAffinity(cores ...int) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network.CPUAffinityCores = append(cfg.Network.CPUAffinityCores, cores...)
	}
}

// WithEarlyHintsHandler registers a callback executed upon receipt of an RFC 8297 103 Early Hints response.
//
// # RFC Compliance
//
// Conforms to RFC 8297 (An HTTP Status Code for Indicating Hints).
func WithEarlyHintsHandler(handler func(hints http.Header)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if handler != nil {
			//nolint:bodyclose // hook observer
			cfg.Defaults.AfterResponse = append(cfg.Defaults.AfterResponse, func(resp *http.Response, err error) {
				if resp != nil && resp.StatusCode == 103 {
					handler(resp.Header)
				}
			})
		}
	}
}
