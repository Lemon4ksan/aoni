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

// Pipeline orchestrates unified request execution across any underlying engine.
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

// Execute runs the complete request execution flow.
func (p *Pipeline) Execute(
	ctx context.Context,
	req Request,
	doer Doer,
	pipe PipelineConfig,
) (*http.Response, error) {
	startTime := time.Now()
	stdReq := p.prepareRequest(req, pipe)

	if cachedResp := p.tryGetFromCache(stdReq, pipe.Cache); cachedResp != nil {
		return cachedResp, nil
	}

	stdReq, traceInfo, traceEnd := p.traceRequest(stdReq, pipe)
	resp, err := p.dispatchRequest(stdReq, doer, pipe)
	duration := time.Since(startTime).Milliseconds()

	for _, hook := range p.defaults.AfterResponse {
		hook(resp, err)
	}

	if traceEnd != nil {
		traceEnd(resp)
	}

	if pipe.Inspect && p.defaults.Inspector != nil {
		p.defaults.Inspector.Capture(stdReq, resp, err, traceInfo)
	}

	if pipe.HAR != nil && pipe.HAR.Tracker != nil {
		pipe.HAR.Tracker.Record(stdReq, resp, startTime, duration)
	}

	if err != nil {
		return nil, err
	}

	p.finalizeJA4Report(GetRequestConfig(stdReq.Context()))

	return p.postProcessResponse(stdReq, resp, pipe)
}

func (p *Pipeline) finalizeJA4Report(cfg *RequestConfig) {
	if cfg == nil || cfg.JA4ReportStore == nil || cfg.JA4ReportStore.Report == nil || cfg.JA4ReportStore.Target == nil {
		return
	}

	store := cfg.JA4ReportStore
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
	}
}

func (p *Pipeline) dispatchRequest(req *http.Request, doer Doer, pipe PipelineConfig) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	switch {
	case pipe.ProxyFailover != nil:
		resp, err = p.executeWithProxyFailover(req, doer, pipe.ProxyFailover, pipe.Hedging)
	case pipe.Hedging != nil:
		resp, err = p.executeWithHedging(req, doer, pipe.Hedging)
	default:
		resp, err = doer.Do(req)
	}

	if resp != nil && resp.Request == nil {
		resp.Request = req
	}

	return resp, err
}
