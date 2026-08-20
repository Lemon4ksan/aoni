// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	stdio "io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

var requestConfigStorage = pool.NewPerPStorage(func() *RequestConfig {
	return &RequestConfig{}
})

type requestConfigKey struct{}

// RequestConfigCtxKey is the exported context key for storing [RequestConfig] in a context.
// Use this when constructing contexts outside internal/pipeline.
type RequestConfigCtxKey = requestConfigKey

// AllocRequestConfig allocates a pooled [RequestConfig] and stores it in ctx, returning the
// enriched context and the config pointer.
func AllocRequestConfig(ctx context.Context) (context.Context, *RequestConfig) {
	if existing := GetRequestConfig(ctx); existing != nil {
		return ctx, existing
	}

	cfg := requestConfigStorage.Get()
	*cfg = RequestConfig{}

	ctx = context.WithValue(ctx, requestConfigKey{}, cfg)

	return ctx, cfg
}

// RedactConfigCtxKey is the context key used to store RedactConfig in the request context.
type RedactConfigCtxKey struct{}

// RequestConfig aggregates request-scoped options and transport overrides.
type RequestConfig struct {
	Network                 string
	Decoder                 core.ResponseDecoder
	ErrorModel              any
	TargetHost              string
	ForceContentType        string
	Label                   string
	UploadProgress          io.ProgressFunc
	DownloadProgress        io.ProgressFunc
	Capturer                any
	BodyError               error
	QueryError              error
	MultipartBoundary       string
	OrderedHeaders          []string
	ALPNOverride            []string
	JA4ReportStore          *JA4ReportStore
	Fallback                core.FallbackFunc
	RequestTimeoutCancel    context.CancelFunc
	HedgingDelayOverride    *time.Duration
	ProxyAddr               *url.URL
	DNSResolver             netdial.DNSResolver
	ResponseValidator       func(resp *http.Response) error
	RetryPolicy             *core.RetryOverride
	P0fSignature            *p0f.Signature
	SessionCache            fingerprint.SessionCache
	PacketPadding           *fingerprint.PaddingConfig
	SocketController        netutil.SocketController
	ClientHelloSpecProvider fingerprint.ClientHelloSpecProvider
	JA4Callback             func(ja4.Report)
	Metadata                map[string]any
	TraceInfo               *telemetry.TraceInfo
	HostRewrite             *netutil.HostRewriteConfig
	Pipeline                *PipelineConfig
	Fragment                *fragment.Config
	Redact                  *RedactConfig
	CertificatePins         map[string][]string
	Modifiers               []core.RequestModifier
	QueryEncoder            core.QueryEncoder
	Decoders                map[string]core.ResponseDecoder

	DisabledFlags    uint32
	UnsafePhaseOrder []PhaseID
	UnsafeHooks      map[PhaseID][]UnsafeHook

	MultiReadThreshold int64
	TimeoutOverride    time.Duration
	CacheTTL           time.Duration
	HappyEyeballsDelay time.Duration
	TCPDelay           netutil.TCPDelayRange

	DisableAltSvc             bool
	Disable0RTT               bool
	MultiReadDisableDisk      bool
	AllowNonReadOnlyHedging   bool
	HasExplicitAcceptEncoding bool
	Debug                     bool
	InsecureSkipVerify        bool
	SSRFGuard                 bool
	ProxyDNS                  bool
	Coalesce                  bool
	ETagAutomaton             bool
	AutoDecode                bool
	DisableBaseResponse       bool
	BaseResponseOverride      func() core.BaseResponse
}

// GetPipeline retrieves the request-specific PipelineConfig from context.
func GetPipeline(ctx context.Context) (PipelineConfig, bool) {
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.Pipeline != nil {
		return *cfg.Pipeline, true
	}

	return PipelineConfig{}, false
}

// LookupDecoder resolves a registered [core.ResponseDecoder] for contentType using request-level decoders or client defaults.
func (cfg *RequestConfig) LookupDecoder(contentType string) core.ResponseDecoder {
	mediaType, _, _ := strings.Cut(contentType, ";")

	norm := strings.ToLower(strings.TrimSpace(mediaType))
	if norm != "" && cfg.Decoders != nil {
		if d, ok := cfg.Decoders[norm]; ok {
			return d
		}
	}

	return nil
}

// GetRequestConfig retrieves the RequestConfig instance attached to the context.
func GetRequestConfig(ctx context.Context) *RequestConfig {
	cfg, _ := ctx.Value(requestConfigKey{}).(*RequestConfig)
	return cfg
}

// GetOrInitRequestConfig retrieves or allocates a [RequestConfig] associated with the provided target.
func GetOrInitRequestConfig(v any) *RequestConfig {
	switch req := v.(type) {
	case core.Request:
		if req == nil {
			return &RequestConfig{}
		}

		cfg := GetRequestConfig(req.Context())
		if cfg == nil {
			cfg = requestConfigStorage.Get()
			*cfg = RequestConfig{}
			ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
			req.SetContext(ctx)
		}

		return cfg

	case *http.Request:
		if req == nil {
			return &RequestConfig{}
		}

		cfg := GetRequestConfig(req.Context())
		if cfg == nil {
			cfg = requestConfigStorage.Get()
			*cfg = RequestConfig{}
			ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
			*req = *req.WithContext(ctx)
		}

		return cfg

	case context.Context:
		cfg := GetRequestConfig(req)
		if cfg == nil {
			cfg = requestConfigStorage.Get()
			*cfg = RequestConfig{}
		}

		return cfg

	default:
		return &RequestConfig{}
	}
}

const maxBodySlurpBytes int64 = 2048

// CloseResponse drains up to 2KB of unread body payload to preserve Keep-Alive sockets,
// closes the response body stream, and recycles request context resources.
func CloseResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	var buf [maxBodySlurpBytes]byte
	if r, ok := resp.Body.(stdio.Reader); ok {
		_, _ = r.Read(buf[:])
	}

	_ = resp.Body.Close()

	if rb, ok := io.UnwrapBody(resp.Body).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}

	if resp.Request == nil {
		return
	}

	cfg := GetRequestConfig(resp.Request.Context())
	if cfg == nil {
		return
	}

	if cfg.RequestTimeoutCancel != nil {
		cfg.RequestTimeoutCancel()
	}
}
