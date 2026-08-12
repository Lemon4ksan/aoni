// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pipeline implements the unified execution orchestrator, transaction state machine,
// connection pool janitors, protocol dispatchers, and Alt-Svc caches.
package pipeline

import (
	"context"
	"net"
	"net/http"
	"time"

	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni/internal/sysnet"
	"github.com/lemon4ksan/aoni/netutil/fragment"
)

// Pipeline orchestrates zero-allocation transaction execution across generic Request/Response engines.
type Pipeline[Req, Resp any] struct {
	defaults    ClientDefaults
	fingerprint ClientFingerprint

	_       cpu.CacheLinePad
	counter uint32
	_       cpu.CacheLinePad
}

type StdPipeline = Pipeline[*http.Request, *http.Response]

func New(defaults ClientDefaults, fingerprint ClientFingerprint) *StdPipeline {
	return &StdPipeline{
		defaults:    defaults,
		fingerprint: fingerprint,
	}
}

func NewGeneric[Req, Resp any](
	defaults ClientDefaults,
	fingerprint ClientFingerprint,
) *Pipeline[Req, Resp] {
	return &Pipeline[Req, Resp]{
		defaults:    defaults,
		fingerprint: fingerprint,
	}
}

// Execute runs the pipeline against the provided request using Fast-Path or Unsafe-Path.
func (p *Pipeline[Req, Resp]) Execute(
	ctx context.Context,
	req Req,
	doer GenericDoer[Req, Resp],
	pipe PipelineConfig,
) (Resp, error) {
	tx := AcquireTx(ctx)
	defer ReleaseTx(tx)

	p.initTx(tx, req, pipe)

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
		} else if aoniResp, ok := any(resp).(Response); ok {
			hook(aoniResp.HTTPResponse(), err) //nolint:bodyclose
		} else {
			hook(nil, err)
		}
	}

	if tx.Flags&FlagInspect != 0 && p.defaults.Inspector != nil {
		var stdReq *http.Request
		if r, ok := any(req).(*http.Request); ok {
			stdReq = r
		} else if rAdapter, okAdapter := any(req).(Request); okAdapter {
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
		} else if rAdapter, okAdapter := any(resp).(Response); okAdapter {
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

		return nil, err
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
		return nil, err
	}

	p.finalizeJA4Report(tx)

	return resp, nil
}

func (p *Pipeline[Req, Resp]) executeCustomPhaseOrder(
	tx *Tx,
	req Req,
	doer GenericDoer[Req, Resp],
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

// ApplyMSSLimit applies TCP MSS limits via OS socket options.
func ApplyMSSLimit(conn net.Conn, mss int) net.Conn {
	if mss <= 0 {
		return conn
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		raw, err := tc.SyscallConn()
		if err != nil {
			return conn
		}

		_ = raw.Control(func(fd uintptr) {
			sysnet.SetTCPMaxSeg(fd, mss)
		})
	}

	return conn
}

// ApplyFragmentation wraps conn with packet chunk fragmentation settings.
func ApplyFragmentation(conn net.Conn, cfg fragment.Config) net.Conn {
	return &fragment.FragmentedConn{
		Conn:      conn,
		ChunkSize: cfg.ChunkSize,
		MaxDelay:  cfg.MaxDelay,
	}
}
