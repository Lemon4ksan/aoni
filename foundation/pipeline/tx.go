// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/foundation/pool"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/telemetry"
)

var txStorage = pool.NewPerPStorage(func() *Tx {
	return &Tx{}
})

// UnsafeHook defines a phase interception hook function executed within custom phase orders.
type UnsafeHook func(tx *Tx, req *http.Request, resp *http.Response) error

// Tx is a pooled transaction state container holding all execution parameters for a single HTTP transaction.
//
// Concurrency & Lifetime Invariants:
// - Tx is checked out from [pool.PerPStorage] at request start and returned upon response completion.
// - It is strictly NOT thread-safe and MUST NOT be accessed concurrently across multiple goroutines.
type Tx struct {
	Ctx context.Context // Strictly used for Cancel/Deadline propagation

	Flags uint32 // Precomputed bitmask for zero-alloc feature checking

	TargetURL            string        // Resolved absolute destination URL
	TargetHost           string        // Cleaned target hostname
	ProxyURL             *url.URL      // Active proxy endpoint for this transaction
	TimeoutOverride      time.Duration // Per-request deadline override
	MultiReadThreshold   int64         // RAM/Disk buffering threshold
	SizeLimit            int64         // Maximum allowed response body bytes
	MultiReadDisableDisk bool          // True to disable temporary file disk buffering

	DPIJitter     *DPIJitterConfig     // TCP handshake packet jitter configuration
	ProxyFailover *ProxyFailoverConfig // Secondary proxy failover policy
	Hedging       *HedgingConfig       // Request hedging racing configuration
	Cache         *CacheConfig         // Response caching parameters
	HAR           *HARConfig           // HAR capture configuration
	Redact        *RedactConfig        // Header redaction rules

	Decoder                 ResponseDecoder                 // Primary response decoder
	ErrorModel              any                             // Structured error target model
	ForceContentType        string                          // Forced MIME content-type override
	Label                   string                          // Metric tracking label
	MultipartBoundary       string                          // Custom multipart boundary
	OrderedHeaders          []string                        // Emulated browser header order
	ALPNOverride            []string                        // Custom TLS ALPN protocol list
	JA4ReportStore          *JA4ReportStore                 // Store for computed JA4 fingerprints
	Fallback                FallbackFunc                    // Failure fallback handler
	ResponseValidator       func(resp *http.Response) error // Custom response validation predicate
	RetryPolicy             *RetryOverride                  // Per-request retry policy
	P0fSignature            *p0f.Signature                  // OS TCP/IP stack signature
	SessionCache            SessionCache                    // Proxy-isolated TLS session cache
	PacketPadding           *fingerprint.PaddingConfig      // DPI packet padding settings
	SocketController        SocketController                // Low-level socket dialer hook
	ClientHelloSpecProvider ClientHelloSpecProvider         // Custom uTLS ClientHello provider
	JA4Callback             func(ja4.Report)                // JA4 computation callback
	Metadata                map[string]any                  // Request metadata store
	TraceInfo               *telemetry.TraceInfo            // Fine-grained request tracer
	HostRewrite             *HostRewriteConfig              // DNS hostname rewrite rules
	Fragment                *fragment.Config                // TCP packet fragmentation configuration
	CertificatePins         map[string][]string             // Domain public key hash pins
	Modifiers               []RequestModifier               // Pipeline modifiers
	QueryEncoder            QueryEncoder                    // Custom query encoder
	Decoders                map[string]ResponseDecoder      // Map of MIME content-type decoders

	// Unsafe mode custom phase sequence
	UnsafePhaseOrder []PhaseID                // Custom phase execution order
	UnsafeHooks      map[PhaseID][]UnsafeHook // Map of phase interceptor hooks
}

// AcquireTx retrieves a clean [Tx] from pool.
func AcquireTx(ctx context.Context) *Tx {
	tx := txStorage.Get()
	*tx = Tx{}
	tx.Ctx = ctx

	return tx
}

// ReleaseTx zeroes fields and returns tx back to pool.
func ReleaseTx(tx *Tx) {
	if tx == nil {
		return
	}

	tx.Ctx = nil
	tx.Flags = 0
	tx.TargetURL = ""
	tx.TargetHost = ""
	tx.ProxyURL = nil
	tx.TimeoutOverride = 0
	tx.MultiReadThreshold = 0
	tx.SizeLimit = 0
	tx.MultiReadDisableDisk = false

	tx.DPIJitter = nil
	tx.ProxyFailover = nil
	tx.Hedging = nil
	tx.Cache = nil
	tx.HAR = nil
	tx.Redact = nil

	tx.Decoder = nil
	tx.ErrorModel = nil
	tx.ForceContentType = ""
	tx.Label = ""
	tx.MultipartBoundary = ""
	tx.OrderedHeaders = nil
	tx.ALPNOverride = nil
	tx.JA4ReportStore = nil
	tx.Fallback = nil
	tx.ResponseValidator = nil
	tx.RetryPolicy = nil
	tx.P0fSignature = nil
	tx.SessionCache = nil
	tx.PacketPadding = nil
	tx.SocketController = nil
	tx.ClientHelloSpecProvider = nil
	tx.JA4Callback = nil
	tx.Metadata = nil
	tx.TraceInfo = nil
	tx.HostRewrite = nil
	tx.Fragment = nil
	tx.CertificatePins = nil
	tx.Modifiers = nil
	tx.QueryEncoder = nil
	tx.Decoders = nil
	tx.UnsafePhaseOrder = nil
	tx.UnsafeHooks = nil

	txStorage.Put(tx)
}

func (p *Pipeline[Req, Resp]) initTx(tx *Tx, pipe PipelineConfig) {
	tx.DPIJitter = pipe.DPIJitter
	tx.ProxyFailover = pipe.ProxyFailover
	tx.Hedging = pipe.Hedging
	tx.Cache = pipe.Cache
	tx.HAR = pipe.HAR
	tx.Redact = pipe.Redact
	tx.SizeLimit = pipe.SizeLimit

	flags := pipe.PrecomputedFlags
	if flags == 0 {
		flags = pipe.BuildFlags()
	}

	if p.defaults.Inspector != nil {
		flags |= FlagInspect
	}

	if pipe.HAR != nil {
		flags |= FlagHAR
	}

	if reqCfg := GetRequestConfig(tx.Ctx); reqCfg != nil {
		tx.TimeoutOverride = reqCfg.TimeoutOverride
		tx.MultiReadThreshold = reqCfg.MultiReadThreshold
		tx.MultiReadDisableDisk = reqCfg.MultiReadDisableDisk
		tx.ProxyURL = reqCfg.ProxyAddr
		tx.JA4ReportStore = reqCfg.JA4ReportStore
		tx.TraceInfo = reqCfg.TraceInfo
		tx.ResponseValidator = reqCfg.ResponseValidator
		tx.UnsafePhaseOrder = reqCfg.UnsafePhaseOrder

		if reqCfg.DisabledFlags != 0 {
			flags &^= reqCfg.DisabledFlags
		}

		tx.UnsafeHooks = reqCfg.UnsafeHooks
	}

	tx.Flags = flags
}
