// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pipeline implements the unified execution orchestrator, transaction state machine,
// connection pool janitors, protocol dispatchers, and Alt-Svc caches.
package pipeline

import (
	"context"
	"errors"
	"net/http"
	"time"

	asyncctx "github.com/lemon4ksan/foundation/async/context"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Pipeline orchestrates the complete HTTP transaction lifecycle across generic Request and Response engines.
//
// Lifecycle Phases:
//  1. Initialization: Acquires a pooled [Tx] transaction state and applies pipeline configs.
//  2. Request Preparation: Applies user modifiers, headers, UA rotation, and packet padding.
//  3. Dispatch: Dispatches the request via Fast-Path, Dynamic Hedging, or standard Doer.
//  4. Post-Processing: Handles auto-recovery (421/408/425), response decompression, and charset transcoding.
//  5. Telemetry & Inspection: Captures network traces, HAR records, and sends data to Inspector.
//  6. Cleanup: Releases [Tx] memory back to the pool with zero allocations.
type Pipeline[Req, Resp any] struct {
	defaults    ClientDefaults
	fingerprint ClientFingerprint

	_       cpu.CacheLinePad
	counter uint32
	_       cpu.CacheLinePad
}

// StdPipeline is a type alias for a [Pipeline] operating on standard [*http.Request] and [*http.Response].
type StdPipeline = Pipeline[*http.Request, *http.Response]

// New instantiates a standard [StdPipeline] configured with the provided defaults and browser fingerprint settings.
func New(defaults ClientDefaults, fingerprint ClientFingerprint) *StdPipeline {
	return &StdPipeline{
		defaults:    defaults,
		fingerprint: fingerprint,
	}
}

// NewGeneric instantiates a generic [Pipeline] capable of operating on custom request/response models.
func NewGeneric[Req, Resp any](
	defaults ClientDefaults,
	fingerprint ClientFingerprint,
) *Pipeline[Req, Resp] {
	return &Pipeline[Req, Resp]{
		defaults:    defaults,
		fingerprint: fingerprint,
	}
}

// Execute orchestrates the full transaction pipeline for the given request and doer.
// Automatically borrows and releases pooled [Tx] transaction state, ensuring zero memory leaks.
func (p *Pipeline[Req, Resp]) Execute(
	ctx context.Context,
	req Req,
	doer core.GenericDoer[Req, Resp],
	pipe PipelineConfig,
) (Resp, error) {
	fastCtx := asyncctx.Wrap(ctx)

	tx := AcquireTx(fastCtx)
	defer ReleaseTx(tx)

	p.initTx(tx, pipe)

	if stdReq, ok := any(req).(*http.Request); ok {
		if stdDoer, okDoer := any(doer).(interface {
			Do(*http.Request) (*http.Response, error)
		}); okDoer {
			stdResp, err := p.executeStandardFastPath(tx, stdReq, stdDoer, time.Now()) //nolint:bodyclose
			if res, okResp := any(stdResp).(Resp); okResp {
				return res, err
			}
		}
	}

	resp, err := doer.Do(req)
	if err != nil {
		for _, hook := range p.defaults.AfterResponse {
			hook(nil, err)
		}

		var zero Resp

		return zero, err
	}

	for _, hook := range p.defaults.AfterResponse {
		if stdResp, ok := any(resp).(*http.Response); ok {
			hook(stdResp, err)
		} else if aoniResp, ok := any(resp).(core.Response); ok {
			hook(aoniResp.HTTPResponse(), err) //nolint:bodyclose
		} else {
			hook(nil, err)
		}
	}

	if tx.Flags&FlagInspect != 0 && p.defaults.Inspector != nil {
		var stdReq *http.Request
		if r, ok := any(req).(*http.Request); ok {
			stdReq = r
		} else if rAdapter, okAdapter := any(req).(core.Request); okAdapter {
			stdReq = rAdapter.HTTPRequest()
			if stdReq == nil {
				stdReq, _ = http.NewRequestWithContext( //nolint:gosec
					rAdapter.Context(),
					rAdapter.Method(),
					rAdapter.URL(),
					nil,
				)
			}
		}

		var stdResp *http.Response
		if r, ok := any(resp).(*http.Response); ok {
			stdResp = r
		} else if rAdapter, okAdapter := any(resp).(core.Response); okAdapter {
			stdResp = rAdapter.HTTPResponse() //nolint:bodyclose
		}

		if stdReq != nil {
			p.defaults.Inspector.Capture(stdReq, stdResp, nil, nil) //nolint:bodyclose
		}
	}

	p.finalizeJA4Report(tx)

	return resp, nil
}

func (p *Pipeline[Req, Resp]) executeStandardFastPath(
	tx *Tx,
	req any,
	doer Doer,
	startTime time.Time,
) (*http.Response, error) {
	stdReq := p.prepareRequest(req, tx)

	if tx.Flags&FlagCache != 0 {
		if cachedResp := p.tryGetFromCache(stdReq, tx.Cache); cachedResp != nil {
			return cachedResp, nil
		}
	}

	stdReq, traceInfo, traceEnd := p.traceRequest(stdReq, tx)
	resp, err := p.dispatchRequest(stdReq, doer, tx) //nolint:bodyclose
	duration := time.Since(startTime).Milliseconds()

	if traceEnd != nil {
		traceEnd(resp)
	}

	if err != nil {
		for _, hook := range p.defaults.AfterResponse {
			hook(nil, err)
		}

		if tx.Flags&FlagInspect != 0 && p.defaults.Inspector != nil {
			p.defaults.Inspector.Capture(stdReq, nil, err, traceInfo)
		}

		return nil, p.enrichError(stdReq, err, traceInfo, time.Since(startTime))
	}

	resp, err = p.postProcessResponse(stdReq, resp, tx) //nolint:bodyclose

	for _, hook := range p.defaults.AfterResponse {
		hook(resp, err)
	}

	if tx.Flags&FlagInspect != 0 && p.defaults.Inspector != nil {
		p.defaults.Inspector.Capture(stdReq, resp, err, traceInfo)
	}

	if tx.Flags&FlagHAR != 0 && tx.HAR != nil && tx.HAR.Tracker != nil {
		tx.HAR.Tracker.Record(stdReq, resp, startTime, duration)
	}

	if err != nil {
		return nil, p.enrichError(stdReq, err, traceInfo, time.Since(startTime))
	}

	p.finalizeJA4Report(tx)

	return resp, nil
}

func (p *Pipeline[Req, Resp]) executeCustomPhaseOrder(
	tx *Tx,
	req Req,
	doer core.GenericDoer[Req, Resp],
	phases []PhaseID,
) (Resp, error) {
	var (
		resp Resp
		err  error
	)

	for _, phase := range phases {
		if hooks, ok := tx.UnsafeHooks[phase]; ok {
			for _, hook := range hooks {
				stdReq, _ := any(req).(*http.Request)
				stdResp, _ := any(resp).(*http.Response)

				if hookErr := hook(tx, stdReq, stdResp); hookErr != nil {
					var zero Resp

					return zero, hookErr
				}
			}
		}

		if phase == PhaseDispatch {
			resp, err = doer.Do(req)
			if err != nil {
				var zero Resp

				return zero, err
			}
		}
	}

	return resp, nil
}

func (p *Pipeline[Req, Resp]) finalizeJA4Report(tx *Tx) {
	if tx == nil || tx.JA4ReportStore == nil || tx.JA4ReportStore.Report == nil || tx.JA4ReportStore.Target == nil {
		return
	}

	store := tx.JA4ReportStore
	if store.Target.JA4 == nil {
		store.Target.JA4 = store.Report
	} else {
		store.Target.JA4.JA4 = store.Report.JA4
		store.Target.JA4.Protocol = store.Report.Protocol
		store.Target.JA4.Version = store.Report.Version
		store.Target.JA4.SNI = store.Report.SNI
		store.Target.JA4.CipherCount = store.Report.CipherCount
		store.Target.JA4.ExtCount = store.Report.ExtCount

		store.Target.JA4.ALPN = store.Report.ALPN
		if store.Report.JA4H != "" {
			store.Target.JA4.JA4H = store.Report.JA4H
		}
	}
}

func (p *Pipeline[Req, Resp]) enrichError(
	req *http.Request,
	err error,
	traceInfo *telemetry.TraceInfo,
	duration time.Duration,
) error {
	if err == nil {
		return nil
	}

	if coreErr, ok := errors.AsType[*core.Error](err); ok {
		if coreErr.URL == "" && req != nil && req.URL != nil {
			coreErr.URL = req.URL.String()
		}

		if coreErr.Op == "" && req != nil {
			coreErr.Op = req.Method
		}

		if coreErr.Duration == 0 {
			coreErr.Duration = duration
		}

		return err
	}

	phase := core.PhaseWaitResponse

	var (
		remoteAddr string
		isReused   bool
		protocol   string
	)

	if traceInfo != nil {
		remoteAddr = traceInfo.RemoteAddr
		isReused = traceInfo.IsReused

		if traceInfo.GotConn.IsZero() {
			if traceInfo.TLSStart.IsZero() {
				phase = core.PhaseTCPConnect
			} else {
				phase = core.PhaseTLSHandshake
			}
		}

		if traceInfo.TLSState != nil {
			protocol = traceInfo.TLSState.NegotiatedProtocol
		}
	}

	op := ""
	urlStr := ""

	if req != nil {
		op = req.Method
		if req.URL != nil {
			urlStr = req.URL.String()
		}
	}

	return &core.Error{
		Op:         op,
		URL:        urlStr,
		Phase:      phase,
		Protocol:   protocol,
		RemoteAddr: remoteAddr,
		IsReused:   isReused,
		Duration:   duration,
		Err:        err,
	}
}
