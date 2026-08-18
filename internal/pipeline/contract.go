// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pipeline implements the zero-allocation transaction execution core.
package pipeline

import (
	"bytes"
	"context"
	"encoding/base64"
	stdio "io"
	"net/http"
	"net/url"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/telemetry"
)

// PhaseID identifies fixed transaction execution phases.
type PhaseID uint8

const (
	PhasePrep PhaseID = iota + 1
	PhaseCacheLookup
	PhaseDispatch
	PhaseDecompress
	PhaseWAF
	PhaseValidate
	PhaseCacheSave
)

// Transaction Flags (bitmask).
const (
	FlagRotateUA uint32 = 1 << iota
	FlagDPIJitter
	FlagRedact
	FlagDecompress
	FlagValidate
	FlagChallenge
	FlagCache
	FlagProxyFailover
	FlagHedging
	FlagInspect
	FlagHAR
	FlagMultiRead
)

const (
	PrepMask = FlagRotateUA | FlagDPIJitter | FlagRedact

	PostProcessMask = FlagDecompress | FlagValidate | FlagChallenge | FlagCache | FlagMultiRead
)

// Request defines the unified execution contract required by the pipeline.
type Request interface {
	// Context management
	Context() context.Context
	SetContext(ctx context.Context)

	// Method and Destination URL
	Method() string
	SetMethod(method string)
	URL() string
	SetURL(urlStr string)
	Path() string
	SetPath(path string)

	// Query Parameters
	RawQuery() string
	SetRawQuery(query string)
	AddQueryParam(key, value string)

	// Header Operations
	Header(key string) string
	SetHeader(key, value string)
	AddHeader(key, value string)
	DelHeader(key string)
	ResetHeaders()

	// Payload Body Operations
	SetBodyBytes(body []byte)
	BodyBytes() []byte
	SetBodyStream(r stdio.Reader, contentLength int64)
	BodyStream() stdio.Reader

	// Underlying Engine Requests
	HTTPRequest() *http.Request
	EngineRequest() any
}

// Response defines the unified response contract required by the pipeline.
type Response interface {
	StatusCode() int
	Status() string
	StatusBytes() []byte

	Header(key string) string
	HeaderBytes(key []byte) []byte
	Headers() map[string][]string

	Trailers() map[string][]string
	SetTrailers(trailers map[string][]string)

	BodyBytes() []byte
	UnsafeBodyBytes() []byte
	BodyStream() stdio.ReadCloser

	HTTPResponse() *http.Response
	EngineResponse() any

	Uncompressed() bool
	SetUncompressed(v bool)
	Close() error
}

type GenericDoer[Req any, Resp any] interface {
	Do(req Req) (Resp, error)
}

type RequestDoer[Req any, Resp any] = GenericDoer[Req, Resp]

type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type DoerFunc func(req *http.Request) (*http.Response, error)

func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type BaseResponse interface {
	IsSuccess() bool
	Error() error
	SetData(data any)
}

type RetryCondition = func(resp Response, err error) bool

type RetryOverride struct {
	Condition   RetryCondition
	Backoff     time.Duration
	MaxAttempts int
}

type FallbackFunc = func(req Request, origErr error) (Response, error)

type PipelineConfig struct {
	DPIJitter          *DPIJitterConfig
	ProxyFailover      *ProxyFailoverConfig
	Hedging            *HedgingConfig
	Cache              *CacheConfig
	HAR                *HARConfig
	Redact             *RedactConfig
	SizeLimit          int64
	MultiReadThreshold int64
	PrecomputedFlags   uint32
	RotateUA           bool
	Inspect            bool
	Decompress         bool
	Validate           bool
	Challenge          bool
}

func (p *PipelineConfig) BuildFlags() uint32 {
	var flags uint32
	if p.RotateUA {
		flags |= FlagRotateUA
	}

	if p.DPIJitter != nil {
		flags |= FlagDPIJitter
	}

	if p.Redact != nil {
		flags |= FlagRedact
	}

	if p.Decompress {
		flags |= FlagDecompress
	}

	if p.Validate {
		flags |= FlagValidate
	}

	if p.Challenge {
		flags |= FlagChallenge
	}

	if p.Cache != nil {
		flags |= FlagCache
	}

	if p.ProxyFailover != nil {
		flags |= FlagProxyFailover
	}

	if p.Hedging != nil {
		flags |= FlagHedging
	}

	if p.Inspect {
		flags |= FlagInspect
	}

	if p.HAR != nil {
		flags |= FlagHAR
	}

	if p.MultiReadThreshold > 0 {
		flags |= FlagMultiRead
	}

	p.PrecomputedFlags = flags

	return flags
}

type DPIJitterConfig struct {
	MinDelay time.Duration
	MaxDelay time.Duration
}

type ProxyFailoverConfig struct {
	Proxies    []string
	RetryLimit int
}

type HedgingConfig struct {
	DynamicHedging       *telemetry.DynamicHedgingConfig
	DefaultDelay         time.Duration
	MaxRequestsPerSecond int
	AllowNonReadOnly     bool
}

type HARConfig struct {
	Tracker interface {
		Record(req *http.Request, resp *http.Response, startTime time.Time, duration int64)
	}
}

type RedactConfig struct {
	Headers          map[string]struct{}
	HeadersToRedact  []string
	JSONKeysToRedact []string
}

type CacheConfig struct {
	Store interface {
		Get(ctx context.Context, key any) ([]byte, error)
		Set(ctx context.Context, key any, val []byte, ttl time.Duration) error
	}
	DefaultTTL    time.Duration
	NoVarySearch  *NoVarySearchConfig
	CookieIndices []string
}

type JA4ReportStore struct {
	Report *ja4.Report
	Target *telemetry.TraceInfo
}

type CacheKey struct {
	Method     string
	URL        string
	CookieHash string
}

func (k CacheKey) String() string {
	totalLen := len(k.Method) + len(k.URL) + 1
	if totalLen <= 128 {
		var buf [128]byte

		n := copy(buf[:], k.Method)
		buf[n] = ':'
		copy(buf[n+1:], k.URL)

		return string(buf[:totalLen])
	}

	return k.Method + ":" + k.URL
}

type CachedResponse struct {
	Header      map[string][]string `json:"header"`
	VaryHeaders map[string]string   `json:"vary_headers,omitempty"`
	BodyBase64  string              `json:"body_base64"`
	StatusCode  int                 `json:"status_code"`
	CachedAt    time.Time           `json:"cached_at"`
}

type TrafficInspector interface {
	Capture(req *http.Request, resp *http.Response, err error, traceInfo *telemetry.TraceInfo)
}

type ChallengeSolver interface {
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

type ClientDefaults struct {
	Headers              http.Header
	BeforeRequest        []func(req *http.Request)
	AfterResponse        []func(resp *http.Response, err error)
	Inspector            TrafficInspector
	ResponseValidator    func(*http.Response) error
	ChallengeDetector    func(*http.Response) (bool, error)
	ChallengeSolver      ChallengeSolver
	UARotationProfiles   []BrowserProfile
	RefererState         *RefererState
	MaxResponseSize      int64
	MultiReadThreshold   int64
	MultiReadDisableDisk bool
	RefererAutomaton     bool
}

type BrowserProfile struct {
	UserAgent   string
	ClientHints map[string]string
}

type RefererState struct {
	Mu      sync.Mutex
	LastURL string
}

type ClientFingerprint struct {
	PacketPadding *fingerprint.PaddingConfig
}

// ModifierType specifies the discrete operation type of a [RequestModifier] value.
type ModifierType uint8

const (
	ModNone ModifierType = iota
	ModHeader
	ModHeaderAdd
	ModQuery
	ModQueryAdd
	ModBearer
	ModBasicAuth
	ModBodyBytes
	ModBodyStream
	ModCustom
)

// RequestModifier represents a zero-allocation value-based modification payload.
type RequestModifier struct {
	Kind        ModifierType
	Key         string
	Value       string
	ContentType string
	Bytes       []byte
	Stream      stdio.Reader
	Fn          func(Request)
}

// Apply mutates an outgoing [Request] contract.
func (m RequestModifier) Apply(req Request) {
	switch m.Kind {
	case ModHeader:
		req.SetHeader(m.Key, m.Value)
	case ModHeaderAdd:
		req.AddHeader(m.Key, m.Value)
	case ModQuery, ModQueryAdd:
		req.AddQueryParam(m.Key, m.Value)
	case ModBearer:
		req.SetHeader("Authorization", "Bearer "+m.Value)
	case ModBasicAuth:
		encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(m.Key + ":" + m.Value))
		req.SetHeader("Authorization", "Basic "+encoded)
	case ModBodyBytes:
		req.SetBodyBytes(m.Bytes)

		if m.ContentType != "" {
			req.SetHeader("Content-Type", m.ContentType)
		}

	case ModBodyStream:
		var lenVal int64 = -1
		if b, ok := m.Stream.(interface{ Len() int }); ok {
			lenVal = int64(b.Len())
		} else if s, ok := m.Stream.(interface{ Len() int64 }); ok {
			lenVal = s.Len()
		}

		req.SetBodyStream(m.Stream, lenVal)

		if m.ContentType != "" {
			req.SetHeader("Content-Type", m.ContentType)
		}

	case ModCustom:
		if m.Fn != nil {
			m.Fn(req)
		}
	}
}

// ApplyStd applies modifications directly to standard *http.Request without wrapper allocations.
func (m RequestModifier) ApplyStd(req *http.Request) {
	switch m.Kind {
	case ModHeader:
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		req.Header.Set(m.Key, m.Value)

	case ModHeaderAdd:
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		req.Header.Add(m.Key, m.Value)

	case ModQuery:
		if req.URL != nil {
			q := req.URL.Query()
			q.Set(m.Key, m.Value)
			req.URL.RawQuery = q.Encode()
		}

	case ModQueryAdd:
		if req.URL != nil {
			q := req.URL.Query()
			q.Add(m.Key, m.Value)
			req.URL.RawQuery = q.Encode()
		}

	case ModBearer:
		if req.Header == nil {
			req.Header = make(http.Header)
		}

		req.Header.Set("Authorization", "Bearer "+m.Value)

	case ModBasicAuth:
		req.SetBasicAuth(m.Key, m.Value)
	case ModBodyBytes:
		buf := m.Bytes
		req.Body = stdio.NopCloser(bytes.NewReader(buf))
		req.GetBody = func() (stdio.ReadCloser, error) {
			return stdio.NopCloser(bytes.NewReader(buf)), nil
		}

		req.ContentLength = int64(len(buf))
		if m.ContentType != "" {
			if req.Header == nil {
				req.Header = make(http.Header)
			}

			req.Header.Set("Content-Type", m.ContentType)
		}

	case ModBodyStream:
		if r, ok := m.Stream.(stdio.ReadCloser); ok {
			req.Body = r
		} else if m.Stream != nil {
			req.Body = stdio.NopCloser(m.Stream)
		}

		if b, ok := m.Stream.(interface{ Len() int }); ok {
			req.ContentLength = int64(b.Len())
		} else if s, ok := m.Stream.(interface{ Len() int64 }); ok {
			req.ContentLength = s.Len()
		}

		if m.ContentType != "" {
			if req.Header == nil {
				req.Header = make(http.Header)
			}

			req.Header.Set("Content-Type", m.ContentType)
		}

	case ModCustom:
		if m.Fn != nil {
			adapter := NewStdRequestAdapter(req)
			m.Fn(adapter)
		}
	}
}

type (
	QueryEncoder func(any) (url.Values, error)
)

type ResponseDecoder interface {
	Decode(reader stdio.Reader, target any) error
}

type SessionCache interface {
	utls.ClientSessionCache
	SetProxyKey(key string)
}

type SocketController interface {
	Control(fd uintptr, network, address string) error
}

type ClientHelloSpecProvider interface {
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

type HostRewriteConfig struct {
	Rules map[string]string
}

type TCPDelayRange struct {
	Min time.Duration
	Max time.Duration
}
