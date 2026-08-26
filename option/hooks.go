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

// WithLogger returns an [aoni.ClientOption] assigning a diagnostic [telemetry.Logger].
func WithLogger(l core.Logger) aoni.ClientOption {
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

// WithInspector returns an [aoni.ClientOption] assigning a [telemetry.TrafficInspector] diagnostic capturer.
func WithInspector(inspector telemetry.TrafficInspector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Inspector = inspector
	}
}

// WithChallengeDetector returns an [aoni.ClientOption] registering a [resiliency.ChallengeDetector] WAF challenge detector.
func WithChallengeDetector(detector resiliency.ChallengeDetector) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.ChallengeDetector = detector
	}
}

// WithChallengeSolver returns an [aoni.ClientOption] registering a [resiliency.ChallengeSolver] WAF challenge solver.
func WithChallengeSolver(solver resiliency.ChallengeSolver) aoni.ClientOption {
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

// WithEarlyHintsHandler returns an [aoni.ClientOption] registering a callback executed
// when an intermediate RFC 8297 103 Early Hints response is received.
func WithEarlyHintsHandler(handler func(hints http.Header)) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if handler != nil {
			cfg.Defaults.AfterResponse = append(cfg.Defaults.AfterResponse, func(resp *http.Response, err error) {
				if resp != nil && resp.StatusCode == 103 {
					handler(resp.Header)
				}
			})
		}
	}
}
