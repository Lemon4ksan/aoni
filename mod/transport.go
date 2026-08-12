// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	stdio "io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

// ============================================================================
// PROTOCOL & NETWORK LAYER MODIFIERS
// ============================================================================

// WithOrderedHeaders constructs an [aoni.RequestModifier] setting HTTP/1.1 wire header serialization sequence.
func WithOrderedHeaders(headers []string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).OrderedHeaders = headers
	})
}

// WithALPN constructs an [aoni.RequestModifier] overriding negotiated ALPN protocols for TLS handshakes.
func WithALPN(protos ...string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = protos
	})
}

// WithoutAltSvc constructs an [aoni.RequestModifier] that disables Alt-Svc connection
// upgrades and IP pooling for a request, forcing direct resolution over a fresh socket.
func WithoutAltSvc() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DisableAltSvc = true
	})
}

// WithForceHTTP1 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/1.1.
func WithForceHTTP1() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnHTTP}
	})
}

// WithForceHTTP2 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/2.
func WithForceHTTP2() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnH2}
	})
}

// WithForceHTTP3 constructs an [aoni.RequestModifier] restricting ALPN negotiation strictly to HTTP/3.
func WithForceHTTP3() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ALPNOverride = []string{aoni.AlpnH3}
	})
}

// Without0RTT constructs an [aoni.RequestModifier] that disables TLS 1.3 / QUIC 0-RTT
// Early Data for a request, forcing standard 1-RTT handshake negotiation.
func Without0RTT() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Disable0RTT = true
	})
}

// WithTCPDelay constructs an [aoni.RequestModifier] adding randomized jitter delays prior to TCP socket dialing.
func WithTCPDelay(min, max time.Duration) aoni.RequestModifier {
	minDelay, maxDelay := min, max
	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).TCPDelay = aoni.TCPDelayRange{Min: minDelay, Max: maxDelay}
	})
}

// WithHappyEyeballs constructs an [aoni.RequestModifier] configuring IPv4/IPv6 stagger delays for request execution.
func WithHappyEyeballs(delay time.Duration) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).HappyEyeballsDelay = delay
	})
}

// WithProxyDNS constructs an [aoni.RequestModifier] routing DNS resolutions through SOCKS5 or HTTP CONNECT proxies.
func WithProxyDNS() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ProxyDNS = true
	})
}

// WithProxyOverride constructs an [aoni.RequestModifier] routing request traffic through a target proxy URL.
func WithProxyOverride(rawURL string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		if u, err := url.Parse(rawURL); err == nil {
			aoni.GetOrInitRequestConfig(req).ProxyAddr = u
		}
	})
}

// WithSSRFGuard constructs an [aoni.RequestModifier] enabling SSRF protections against loopback and private IP addresses.
func WithSSRFGuard() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SSRFGuard = true
	})
}

// WithInsecureSkipVerify constructs an [aoni.RequestModifier] bypassing TLS peer certificate verification for the request.
func WithInsecureSkipVerify() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).InsecureSkipVerify = true
	})
}

// WithFragmentation constructs an [aoni.RequestModifier] configuring TCP packet fragmentation parameters.
func WithFragmentation(cfg fragment.Config) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Fragment = &cfg
	})
}

// WithFragment is an alias for [WithFragmentation].
func WithFragment(cfg fragment.Config) aoni.RequestModifier {
	return WithFragmentation(cfg)
}

// WithHostRewrite constructs an [aoni.RequestModifier] replacing host DNS remapping rules for the request.
func WithHostRewrite(rules map[string]string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).HostRewrite = &pipeline.HostRewriteConfig{Rules: rules}
	})
}

// WithAppendHostRewrite constructs an [aoni.RequestModifier] appending new DNS remapping rules to existing request settings.
func WithAppendHostRewrite(rules map[string]string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		newRules := make(map[string]string, len(rules))
		if cfg.HostRewrite != nil && cfg.HostRewrite.Rules != nil {
			maps.Copy(newRules, cfg.HostRewrite.Rules)
		}

		maps.Copy(newRules, rules)
		cfg.HostRewrite = &pipeline.HostRewriteConfig{Rules: newRules}
	})
}

// WithSocketController constructs an [aoni.RequestModifier] assigning a low-level socket controller callback.
func WithSocketController(controller aoni.SocketController) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SocketController = controller
	})
}

// WithP0fSignature constructs an [aoni.RequestModifier] setting p0f TCP stack signature parameters.
func WithP0fSignature(sig *p0f.Signature) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).P0fSignature = sig
	})
}

// WithSessionCache constructs an [aoni.RequestModifier] assigning an isolated proxy-aware TLS [aoni.SessionCache].
func WithSessionCache(cache aoni.SessionCache) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SessionCache = cache
	})
}

// WithCertificatePin constructs an [aoni.RequestModifier] pinning SHA-256 public key hashes for target domains.
func WithCertificatePin(domain, hash string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], hash)
	})
}

// WithPadding constructs an [aoni.RequestModifier] injecting random packet padding headers to confuse DPI length analysis.
func WithPadding(cfg fingerprint.PaddingConfig) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		if padding := fingerprint.GeneratePadding(cfg); len(padding) > 0 {
			headerName := fingerprint.PaddingHeaderName(cfg)
			req.SetHeader(headerName, hex.EncodeToString(padding))
		}
	})
}

// ============================================================================
// PIPELINE, RESILIENCE & CONTEXT MODIFIERS
// ============================================================================

// WithContext constructs an [aoni.RequestModifier] updating the execution context associated with the request.
func WithContext(ctx context.Context) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		req.SetContext(ctx)
	})
}

// WithTimeout constructs an [aoni.RequestModifier] attaching a deadline timeout to the request context.
func WithTimeout(d time.Duration) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), d) //nolint:gosec
		req.SetContext(ctx)
		aoni.GetOrInitRequestConfig(req).RequestTimeoutCancel = cancel
	})
}

// WithPipeline constructs an [aoni.RequestModifier] overriding execution pipeline configurations for the request.
func WithPipeline(pipe aoni.PipelineConfig) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		p := toInternalPipelineConfig(pipe)
		aoni.GetOrInitRequestConfig(req).Pipeline = &p
	})
}

func toInternalPipelineConfig(p aoni.PipelineConfig) pipeline.PipelineConfig {
	res := pipeline.PipelineConfig{
		SizeLimit:          p.SizeLimit,
		MultiReadThreshold: p.MultiReadThreshold,
		RotateUA:           p.RotateUA,
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
		Challenge:          p.Challenge,
	}
	if p.DPIJitter != nil {
		res.DPIJitter = &pipeline.DPIJitterConfig{
			MinDelay: p.DPIJitter.MinDelay,
			MaxDelay: p.DPIJitter.MaxDelay,
		}
	}

	if p.ProxyFailover != nil {
		res.ProxyFailover = &pipeline.ProxyFailoverConfig{
			Proxies:    p.ProxyFailover.Proxies,
			RetryLimit: p.ProxyFailover.RetryLimit,
		}
	}

	if p.Hedging != nil {
		res.Hedging = &pipeline.HedgingConfig{
			DynamicHedging:       p.Hedging.DynamicHedging,
			DefaultDelay:         p.Hedging.DefaultDelay,
			MaxRequestsPerSecond: p.Hedging.MaxRequestsPerSecond,
			AllowNonReadOnly:     p.Hedging.AllowNonReadOnly,
		}
	}

	if p.Cache != nil {
		var nvs *pipeline.NoVarySearchConfig
		if p.Cache.NoVarySearch != nil {
			nvs = &pipeline.NoVarySearchConfig{
				IgnoreParams:    p.Cache.NoVarySearch.IgnoreParams,
				ExceptParams:    p.Cache.NoVarySearch.ExceptParams,
				IgnoreAllParams: p.Cache.NoVarySearch.IgnoreAllParams,
			}
		}

		res.Cache = &pipeline.CacheConfig{
			Store:         p.Cache.Store,
			DefaultTTL:    p.Cache.DefaultTTL,
			NoVarySearch:  nvs,
			CookieIndices: p.Cache.CookieIndices,
		}
	}

	if p.HAR != nil {
		res.HAR = &pipeline.HARConfig{
			Tracker: p.HAR.Tracker,
		}
	}

	if p.Redact != nil {
		res.Redact = &pipeline.RedactConfig{
			Headers:          p.Redact.Headers,
			HeadersToRedact:  p.Redact.HeadersToRedact,
			JSONKeysToRedact: p.Redact.JSONKeysToRedact,
		}
	}

	res.BuildFlags()

	return res
}

// PhaseID identifies fixed transaction execution phases.
type PhaseID = pipeline.PhaseID

const (
	PhasePrep        = pipeline.PhasePrep
	PhaseCacheLookup = pipeline.PhaseCacheLookup
	PhaseDispatch    = pipeline.PhaseDispatch
	PhaseDecompress  = pipeline.PhaseDecompress
	PhaseWAF         = pipeline.PhaseWAF
	PhaseValidate    = pipeline.PhaseValidate
	PhaseCacheSave   = pipeline.PhaseCacheSave
)

// WithUnsafePhaseOrder sets a custom phase order for the pipeline.
func WithUnsafePhaseOrder(phases ...PhaseID) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).UnsafePhaseOrder = phases
	})
}

// WithUnsafeDisableFlags allows to disable pipeline phases instantly (by clearing bits in 1 CPU cycle).
// Example: mod.WithUnsafeDisableFlags(pipeline.FlagChallenge | pipeline.FlagCache)
func WithUnsafeDisableFlags(flags uint32) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		cfg.DisabledFlags |= flags
	})
}

// WithUnsafeHook inserts a zero-allocation hook before the specified pipeline phase.
func WithUnsafeHook(phase pipeline.PhaseID, hook pipeline.UnsafeHook) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.UnsafeHooks == nil {
			cfg.UnsafeHooks = make(map[pipeline.PhaseID][]pipeline.UnsafeHook)
		}

		cfg.UnsafeHooks[phase] = append(cfg.UnsafeHooks[phase], hook)
	})
}

// WithRetryPolicy constructs an [aoni.RequestModifier] assigning custom retry parameters to the request.
func WithRetryPolicy(override aoni.RetryOverride) aoni.RequestModifier {
	policy := override
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}

	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).RetryPolicy = &policy
	})
}

// WithAllowNonReadOnlyHedging constructs an [aoni.RequestModifier] permitting request hedging for non-idempotent HTTP methods.
func WithAllowNonReadOnlyHedging(allow bool) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).AllowNonReadOnlyHedging = allow
	})
}

// WithFallback constructs an [aoni.RequestModifier] registering an alternative response fallback generator.
func WithFallback(f aoni.FallbackFunc) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Fallback = f
	})
}

// WithResponseValidator constructs an [aoni.RequestModifier] attaching custom response validation predicates.
func WithResponseValidator(fn func(resp *http.Response) error) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

		existing := cfg.ResponseValidator
		if existing != nil {
			cfg.ResponseValidator = func(resp *http.Response) error {
				if err := existing(resp); err != nil {
					return err
				}

				return fn(resp)
			}

			return
		}

		cfg.ResponseValidator = fn
	})
}

// WithMultiReadThreshold constructs an [aoni.RequestModifier] configuring RAM buffering bounds for replayable reads.
func WithMultiReadThreshold(threshold int64) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadThreshold = threshold
	})
}

// WithMultiReadDisableDisk constructs an [aoni.RequestModifier] disabling temporary file disk backing on buffer overflows.
func WithMultiReadDisableDisk(disable bool) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadDisableDisk = disable
	})
}

// WithCacheTTL constructs an [aoni.RequestModifier] configuring custom response caching TTL for the request.
func WithCacheTTL(ttl time.Duration) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).CacheTTL = ttl
	})
}

// WithRedact constructs an [aoni.RequestModifier] configuring header and key redaction rules for logging.
func WithRedact(cfg aoni.RedactConfig) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		r := pipeline.RedactConfig{
			Headers:          cfg.Headers,
			HeadersToRedact:  cfg.HeadersToRedact,
			JSONKeysToRedact: cfg.JSONKeysToRedact,
		}
		aoni.GetOrInitRequestConfig(req).Redact = &r
	})
}

// WithConnMetadata constructs an [aoni.RequestModifier] associating custom key-value metadata with the request connection.
func WithConnMetadata(key string, val any) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any)
		}

		cfg.Metadata[key] = val
	})
}

// WithForceContentType constructs an [aoni.RequestModifier] forcing response decoding via a specific MIME type.
func WithForceContentType(mime string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ForceContentType = mime
	})
}

// WithErrorModel constructs an [aoni.RequestModifier] assigning a target struct pointer for non-2xx API error response unmarshaling.
func WithErrorModel(model any) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ErrorModel = model
	})
}

// WithDecoder constructs an [aoni.RequestModifier] overriding the response decoder implementation for the request.
func WithDecoder(d aoni.ResponseDecoder) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Decoder = d
	})
}

// WithUploadProgress constructs an [aoni.RequestModifier] registering an upload progress tracking callback.
func WithUploadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).UploadProgress = progress
	})
}

// WithDownloadProgress constructs an [aoni.RequestModifier] registering a download progress tracking callback.
func WithDownloadProgress(progress aoni.ProgressFunc) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DownloadProgress = progress
	})
}

// WithCaptureResponse constructs an [aoni.RequestModifier] capturing a reference pointer to the raw [*http.Response].
func WithCaptureResponse(target any) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Capturer = target
	})
}

// ============================================================================
// TELEMETRY, TRACING & DIAGNOSTICS MODIFIERS
// ============================================================================

// WithCorrelationID constructs an [aoni.RequestModifier] setting an end-to-end tracing correlation ID header ("X-Correlation-ID").
func WithCorrelationID(id string) aoni.RequestModifier {
	activeID := id
	if activeID == "" {
		activeID = telemetry.GenerateCorrelationID()
	}

	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.TraceInfo != nil {
			cfg.TraceInfo.CorrelationID = activeID
		}

		req.SetHeader("X-Correlation-ID", activeID)
	})
}

// WithLabel constructs an [aoni.RequestModifier] assigning a route or metric label to the request context.
func WithLabel(label string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		cfg.Label = label

		if cfg.TraceInfo != nil {
			cfg.TraceInfo.Label = label
		}
	})
}

// WithDebug constructs an [aoni.RequestModifier] marking the request for verbose diagnostic logging.
func WithDebug() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Debug = true
	})
}

// WithCurlDump constructs an [aoni.RequestModifier] printing an equivalent shell-escaped cURL command to stderr.
func WithCurlDump() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		stdReq := req.HTTPRequest()
		if stdReq != nil {
			dumpStdRequest(stdReq)
			return
		}

		dumpGenericRequest(req)
	})
}

func dumpStdRequest(stdReq *http.Request) {
	var body []byte
	if stdReq.Body != nil && stdReq.Body != http.NoBody {
		var buf bytes.Buffer

		_, _ = io.CopyZeroAlloc(&buf, stdReq.Body)
		body = buf.Bytes()
		stdReq.Body = stdio.NopCloser(bytes.NewReader(body))
	}

	curl := telemetry.CurlFromRequest(stdReq, body)
	fmt.Fprintf(os.Stderr, "%s\n", curl)
}

func dumpGenericRequest(req aoni.Request) {
	body := req.BodyBytes()

	dummyReq, _ := http.NewRequest(req.Method(), req.URL(), bytes.NewReader(body)) //nolint:noctx
	if dummyReq != nil {
		curl := telemetry.CurlFromRequest(dummyReq, body)
		fmt.Fprintf(os.Stderr, "%s\n", curl)
	}
}

// WithTrace constructs an [aoni.RequestModifier] assigning a connection tracer container to capture connection metrics.
func WithTrace(target *telemetry.TraceInfo) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).TraceInfo = target
	})
}

// WithTraceJA4 constructs an [aoni.RequestModifier] enabling JA4/JA4H client fingerprint telemetry.
func WithTraceJA4(target *telemetry.TraceInfo) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		if target.JA4 == nil {
			target.JA4 = &ja4.Report{}
		}

		store := &aoni.JA4ReportStore{Report: target.JA4, Target: target}
		aoni.GetOrInitRequestConfig(req).JA4ReportStore = store

		if stdReq := req.HTTPRequest(); stdReq != nil {
			target.JA4.JA4H = telemetry.ComputeJA4HFromRequest(stdReq)
		}
	})
}

// WithTraceContext constructs an [aoni.RequestModifier] attaching a new [telemetry.TraceInfo] container to the request context.
func WithTraceContext() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		info := &telemetry.TraceInfo{}
		aoni.GetOrInitRequestConfig(req).TraceInfo = info
		WithTraceJA4(info).Apply(req)
	})
}

// WithJA4Callback constructs an [aoni.RequestModifier] setting a callback executed with the computed [ja4.Report] after TLS handshakes.
func WithJA4Callback(fn func(ja4.Report)) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).JA4Callback = fn
	})
}

// WithClientHelloSpecProvider constructs an [aoni.RequestModifier] assigning a dynamic uTLS spec provider.
func WithClientHelloSpecProvider(provider aoni.ClientHelloSpecProvider) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ClientHelloSpecProvider = provider
	})
}

// WithDNSResolver constructs an [aoni.RequestModifier] assigning a per-request custom DNS resolver override.
func WithDNSResolver(resolver netdial.DNSResolver) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DNSResolver = resolver
	})
}

// WithoutBaseResponse constructs an [aoni.RequestModifier] disabling base response allocation for maximum RPS.
func WithoutBaseResponse() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).DisableBaseResponse = true
	})
}
