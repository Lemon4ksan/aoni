// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/telemetry"
)

var txPool = sync.Pool{
	New: func() any { return &Tx{} },
}

type UnsafeHook func(tx *Tx, req *http.Request, resp *http.Response) error

// Tx is a pooled transaction object holding all execution parameters.
type Tx struct {
	Ctx context.Context // Strictly used for Cancel/Deadline propagation!

	Flags uint32 // Bitmask for zero-alloc feature checking

	TargetURL            string
	TargetHost           string
	ProxyURL             *url.URL
	TimeoutOverride      time.Duration
	MultiReadThreshold   int64
	SizeLimit            int64
	MultiReadDisableDisk bool

	DPIJitter     *DPIJitterConfig
	ProxyFailover *ProxyFailoverConfig
	Hedging       *HedgingConfig
	Cache         *CacheConfig
	HAR           *HARConfig
	Redact        *RedactConfig

	Decoder                 ResponseDecoder
	ErrorModel              any
	ForceContentType        string
	Label                   string
	MultipartBoundary       string
	OrderedHeaders          []string
	ALPNOverride            []string
	JA4ReportStore          *JA4ReportStore
	Fallback                FallbackFunc
	ResponseValidator       func(resp *http.Response) error
	RetryPolicy             *RetryOverride
	P0fSignature            *p0f.Signature
	SessionCache            SessionCache
	PacketPadding           *fingerprint.PaddingConfig
	SocketController        SocketController
	ClientHelloSpecProvider ClientHelloSpecProvider
	JA4Callback             func(ja4.Report)
	Metadata                map[string]any
	TraceInfo               *telemetry.TraceInfo
	HostRewrite             *HostRewriteConfig
	Fragment                *fragment.Config
	CertificatePins         map[string][]string
	Modifiers               []RequestModifier
	QueryEncoder            QueryEncoder
	Decoders                map[string]ResponseDecoder

	// Unsafe mode custom phase sequence
	UnsafePhaseOrder []PhaseID
	UnsafeHooks      map[PhaseID][]UnsafeHook
}

// AcquireTx fetches a clean Tx instance from pool.
func AcquireTx(ctx context.Context) *Tx {
	tx := txPool.Get().(*Tx)
	tx.Ctx = ctx
	return tx
}

// ReleaseTx returns the Tx instance to pool after resetting fields.
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

	txPool.Put(tx)
}

func (p *Pipeline) initTx(tx *Tx, req Request, pipe PipelineConfig) {
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
		for _, mod := range reqCfg.Modifiers {
			if mod != nil {
				mod(req)
			}
		}

		reqCfg.Modifiers = nil

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
