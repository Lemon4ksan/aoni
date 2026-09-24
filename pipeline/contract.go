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

	"github.com/lemon4ksan/aoni/netutil/dict"
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
	PrepMask = FlagRedact

	PostProcessMask = FlagDecompress | FlagValidate | FlagCache | FlagMultiRead
)

// Doer represents an abstraction for executing an HTTP request.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type DoerFunc func(req *http.Request) (*http.Response, error)

// Do calls f(req).
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// PipelineConfig holds execution parameters for a single pipeline run.
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

// BuildFlags computes the combined optimization bitmask for this configuration.
func (p *PipelineConfig) BuildFlags() uint32 {
	var flags uint32

	if p.Redact != nil {
		flags |= FlagRedact
	}

	if p.Decompress {
		flags |= FlagDecompress
	}

	if p.Validate {
		flags |= FlagValidate
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

// DPIJitterConfig controls Deep Packet Inspection evasion strategies.
type DPIJitterConfig struct {
	MinDelay time.Duration
	MaxDelay time.Duration
}

// ProxyFailoverConfig specifies fallback proxies and retry behavior.
type ProxyFailoverConfig struct {
	Proxies    []string
	RetryLimit int
}

// HedgingConfig defines parameters for dynamic request hedging (racing).
type HedgingConfig struct {
	DynamicHedging       *telemetry.DynamicHedgingConfig
	DefaultDelay         time.Duration
	MaxRequestsPerSecond int
	AllowNonReadOnly     bool
}

// HARConfig controls HTTP Archive (HAR) telemetry tracking.
type HARConfig struct {
	Tracker interface {
		Record(req *http.Request, resp *http.Response, startTime time.Time, duration int64)
	}
}

// RedactConfig defines rules for redacting sensitive telemetry data.
type RedactConfig struct {
	Headers          map[string]struct{}
	HeadersToRedact  []string
	JSONKeysToRedact []string
}

// CacheConfig controls the behavior of RFC 9111 HTTP caching.
type CacheConfig struct {
	Store interface {
		Get(ctx context.Context, key any) ([]byte, error)
		Set(ctx context.Context, key any, val []byte, ttl time.Duration) error
	}
	DefaultTTL    time.Duration
	NoVarySearch  *NoVarySearchConfig
	CookieIndices []string
}

// CacheKey uniquely identifies a cached response.
type CacheKey struct {
	Method     string
	URL        string
	CookieHash string
}

// String returns a string representation of the cache key.
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

// CachedResponse represents a serialized HTTP response in cache.
type CachedResponse struct {
	Header      map[string][]string `json:"header"`
	VaryHeaders map[string]string   `json:"vary_headers,omitempty"`
	BodyBase64  string              `json:"body_base64"`
	StatusCode  int                 `json:"status_code"`
	CachedAt    time.Time           `json:"cached_at"`
}

// ClientDefaults holds the default configurations for a Pipeline.
type ClientDefaults struct {
	Headers                      http.Header
	BeforeRequest                []func(req *http.Request)
	AfterResponse                []func(resp *http.Response, err error)
	Inspector                    telemetry.TrafficInspector
	ResponseValidators           []func(*http.Response) error
	SoftErrorDetectors           []func(*http.Response, []byte) error
	UARotationProfiles           []BrowserProfile
	RefererState                 *RefererState
	MaxResponseSize              int64
	MultiReadThreshold           int64
	MultiReadDisableDisk         bool
	RefererAutomaton             bool
	DictionaryStore              *dict.Store
	DisableDictionaryCompression bool
}

// BrowserProfile represents an evasive browser fingerprinting configuration.
type BrowserProfile struct {
	UserAgent   string
	ClientHints map[string]string
}

// RefererState maintains the state for automatic Referer header injection.
type RefererState struct {
	LastURL generic.Safe[string]
}
