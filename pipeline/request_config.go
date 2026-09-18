// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni/x/telemetry"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/dict"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

var requestConfigStorage = pool.NewPerPStorage(func() *RequestConfig {
	return &RequestConfig{}
})

// RequestConfigKey is the internal context key type for RequestConfig.
type RequestConfigKey struct{}

// RequestConfigCtxKey is the exported context key for storing [RequestConfig] in a context.
// Use this when constructing contexts outside internal/pipeline.
type RequestConfigCtxKey = RequestConfigKey

// AllocRequestConfig allocates a pooled [RequestConfig] and stores it in ctx, returning the
// enriched context and the config pointer.
// If ctx is nil, context.Background() is used as the base context.
func AllocRequestConfig(ctx context.Context) (context.Context, *RequestConfig) {
	if ctx == nil {
		ctx = context.Background()
	}

	if existing := GetRequestConfig(ctx); existing != nil {
		return ctx, existing
	}

	cfg := requestConfigStorage.Get()
	*cfg = RequestConfig{}

	ctx = context.WithValue(ctx, RequestConfigKey{}, cfg)

	return ctx, cfg
}

// RedactConfigCtxKey is the context key used to store RedactConfig in the request context.
type RedactConfigCtxKey struct{}

// RequestConfig aggregates request-scoped options and transport overrides.
type RequestConfig struct {
	Network              string
	Decoder              core.ResponseDecoder
	ErrorModel           any
	TargetHost           string
	ForceContentType     string
	Label                string
	UploadProgress       iokit.ProgressFunc
	DownloadProgress     iokit.ProgressFunc
	Capturer             any
	BodyError            error
	QueryError           error
	MultipartBoundary    string
	OrderedHeaders       []string
	ALPNOverride         []string
	Fallback             core.FallbackFunc
	RequestTimeoutCancel context.CancelFunc
	HedgingDelayOverride *time.Duration
	ProxyAddr            *url.URL
	DNSResolver          netdial.DNSResolver
	ResponseValidators   []func(resp *http.Response) error
	SoftErrorDetectors   []func(*http.Response, []byte) error
	RetryPolicy          *core.RetryOverride
	SocketController     netutil.SocketController
	Metadata             map[string]any
	TraceInfo            *telemetry.TraceInfo
	HostRewrite          *netutil.HostRewriteConfig
	Pipeline             *PipelineConfig
	Fragment             *fragment.Config
	Redact               *RedactConfig
	Modifiers            []core.RequestModifier
	QueryEncoder         core.QueryEncoder
	Decoders             map[string]core.ResponseDecoder

	DisabledFlags    uint32
	UnsafePhaseOrder []PhaseID
	UnsafeHooks      map[PhaseID][]UnsafeHook

	MultiReadThreshold int64
	TimeoutOverride    time.Duration
	CacheTTL           time.Duration
	HappyEyeballsDelay time.Duration
	TCPDelay           netutil.TCPDelayRange

	AvailableDictionary          *dict.Dictionary
	DictionaryStore              *dict.Store
	DisableDictionaryCompression bool
	DisableAltSvc                bool
	Disable0RTT                  bool
	MultiReadDisableDisk         bool
	AllowNonReadOnlyHedging      bool
	HasExplicitAcceptEncoding    bool
	Debug                        bool
	InsecureSkipVerify           bool
	SSRFGuard                    bool
	ProxyDNS                     bool
	Coalesce                     bool
	ETagAutomaton                bool
	AutoDecode                   bool
	DisableBaseResponse          bool
	SpecialRecoveryDone          bool
	BaseResponseOverride         func() core.BaseResponse
}

// GetPipelineConfig retrieves the request-specific PipelineConfig from context.
func GetPipelineConfig(ctx context.Context) (PipelineConfig, bool) {
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

// GetRequestConfig retrieves the RequestConfig instance attached to the context or request.
// Returns nil if no RequestConfig is attached.
func GetRequestConfig(v any) *RequestConfig {
	if v == nil {
		return nil
	}

	switch req := v.(type) {
	case core.Request:
		if req != nil {
			if cfg, ok := req.Config().(*RequestConfig); ok && cfg != nil {
				return cfg
			}

			ctx := req.Context()
			if ctx == nil || any(ctx) == any(req) {
				return nil
			}

			return GetRequestConfig(ctx)
		}

	case *http.Request:
		if req != nil {
			return GetRequestConfig(req.Context())
		}
	case context.Context:
		cfg, _ := req.Value(RequestConfigKey{}).(*RequestConfig)
		return cfg
	}

	return nil
}

// GetOrInitRequestConfig retrieves or allocates a [RequestConfig] associated with the provided target.
func GetOrInitRequestConfig(v any) *RequestConfig {
	switch req := v.(type) {
	case core.Request:
		if req == nil {
			return &RequestConfig{}
		}

		if cfg, ok := req.Config().(*RequestConfig); ok && cfg != nil {
			return cfg
		}

		cfg := GetRequestConfig(req)
		if cfg == nil {
			cfg = requestConfigStorage.Get()
			*cfg = RequestConfig{}
			req.SetConfig(cfg)
		} else {
			req.SetConfig(cfg)
		}

		return cfg

	case *http.Request:
		if req == nil {
			return &RequestConfig{}
		}

		cfg := GetRequestConfig(req)
		if cfg == nil {
			cfg = requestConfigStorage.Get()
			*cfg = RequestConfig{}
			ctx := context.WithValue(req.Context(), RequestConfigKey{}, cfg)
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
	if r, ok := resp.Body.(io.Reader); ok {
		_, _ = r.Read(buf[:])
	}

	_ = resp.Body.Close()

	if rb, ok := iokit.UnwrapBody(resp.Body).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}

	if resp.Request == nil {
		return
	}

	cfg := GetRequestConfig(resp.Request)
	if cfg == nil {
		return
	}

	if cfg.RequestTimeoutCancel != nil {
		cfg.RequestTimeoutCancel()
	}
}
