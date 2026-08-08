// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/sys/cpu"
)

// Pipeline orchestrates zero-allocation transaction execution.
type Pipeline struct {
	defaults    ClientDefaults
	fingerprint ClientFingerprint

	_       cpu.CacheLinePad
	counter uint32
	_       cpu.CacheLinePad
}

func NewPipeline(defaults ClientDefaults, fingerprint ClientFingerprint) *Pipeline {
	return &Pipeline{
		defaults:    defaults,
		fingerprint: fingerprint,
	}
}

// Execute runs the pipeline against the provided request using Fast-Path or Unsafe-Path.
func (p *Pipeline) Execute(
	ctx context.Context,
	req Request,
	doer Doer,
	pipe PipelineConfig,
) (*http.Response, error) {
	tx := AcquireTx(ctx)
	defer ReleaseTx(tx)

	p.initTx(tx, req, pipe)

	if len(tx.UnsafePhaseOrder) == 0 {
		return p.executeStandardFastPath(tx, req, doer, time.Now())
	}

	return p.executeCustomPhaseOrder(tx, req, doer, tx.UnsafePhaseOrder)
}

func (p *Pipeline) executeStandardFastPath(
	tx *Tx,
	req Request,
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
	resp, err := p.dispatchRequest(stdReq, doer, tx)
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

		return nil, err
	}

	resp, err = p.postProcessResponse(stdReq, resp, tx)

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
		return nil, err
	}

	p.finalizeJA4Report(tx)

	return resp, nil
}

func (p *Pipeline) executeCustomPhaseOrder(
	tx *Tx,
	req Request,
	doer Doer,
	phases []PhaseID,
) (*http.Response, error) {
	var (
		stdReq *http.Request
		resp   *http.Response
		err    error
	)

	for _, phase := range phases {
		if hooks, ok := tx.UnsafeHooks[phase]; ok {
			for _, hook := range hooks {
				if hookErr := hook(tx, stdReq, resp); hookErr != nil {
					return nil, hookErr
				}
			}
		}

		switch phase {
		case PhasePrep:
			stdReq = p.prepareRequest(req, tx)

		case PhaseCacheLookup:
			if stdReq != nil && tx.Flags&FlagCache != 0 {
				if cached := p.tryGetFromCache(stdReq, tx.Cache); cached != nil {
					return cached, nil
				}
			}

		case PhaseDispatch:
			if stdReq == nil {
				stdReq = p.prepareRequest(req, tx)
			}

			resp, err = p.dispatchRequest(stdReq, doer, tx)
			if err != nil {
				return nil, err
			}

		case PhaseDecompress:
			if resp != nil && tx.Flags&FlagDecompress != 0 {
				resp = p.handleDecompressionAndTranscoding(stdReq, resp)
			}

		case PhaseWAF:
			if resp != nil && tx.Flags&FlagChallenge != 0 {
				resp, err = p.handleWAFChallenge(stdReq, resp)
				if err != nil {
					return nil, err
				}
			}

		case PhaseValidate:
			if resp != nil && tx.Flags&FlagValidate != 0 {
				if valErr := p.validateResponse(resp, tx); valErr != nil {
					return nil, valErr
				}
			}

		case PhaseCacheSave:
			if stdReq != nil && resp != nil && tx.Flags&FlagCache != 0 {
				p.saveToCache(stdReq, resp, tx.Cache)
			}
		}
	}

	return resp, nil
}

func (p *Pipeline) finalizeJA4Report(tx *Tx) {
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
