// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pipeline implements the zero-allocation transaction execution core.
package pipeline

import (
	"context"
	"net/http"
	"time"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
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

type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type DoerFunc func(req *http.Request) (*http.Response, error)

func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

type ClientDefaults struct {
	Headers              http.Header
	BeforeRequest        []func(req *http.Request)
	AfterResponse        []func(resp *http.Response, err error)
	Inspector            telemetry.TrafficInspector
	ResponseValidator    func(*http.Response) error
	SoftErrorDetectors   []func(*http.Response, []byte) error
	ChallengeDetector    func(*http.Response) (bool, error)
	ChallengeSolver      challenge.Solver
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
	LastURL generic.Safe[string]
}

type ClientFingerprint struct {
	PacketPadding *fingerprint.PaddingConfig
}
