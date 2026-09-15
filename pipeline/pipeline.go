// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pipeline implements the unified execution orchestrator, transaction state machine,
// connection pool janitors, protocol dispatchers, and Alt-Svc caches.
package pipeline

import (
	"context"
	"net/http"
	"time"

	"github.com/lemon4ksan/foundation/async/ctxkit"

	"github.com/lemon4ksan/aoni/internal/core"
)

// Pipeline orchestrates the complete transaction lifecycle across generic request and response models.
type Pipeline[Req, Resp any] struct {
	handler PipelineHandler[Req, Resp]
}

// StdPipeline is a type alias for a [Pipeline] operating on standard [*http.Request] and [*http.Response].
type StdPipeline = Pipeline[*http.Request, *http.Response]

// New instantiates a standard [StdPipeline] configured with the provided defaults.
func New(defaults ClientDefaults) *StdPipeline {
	return &Pipeline[*http.Request, *http.Response]{
		handler: NewStdHandler(defaults),
	}
}

// NewGeneric instantiates a generic [Pipeline] capable of operating on custom request/response models.
func NewGeneric[Req, Resp any](handler PipelineHandler[Req, Resp]) *Pipeline[Req, Resp] {
	return &Pipeline[Req, Resp]{
		handler: handler,
	}
}

// Execute orchestrates the full transaction pipeline for the given request and doer.
func (p *Pipeline[Req, Resp]) Execute(
	ctx context.Context,
	req Req,
	doer core.GenericDoer[Req, Resp],
	pipe PipelineConfig,
) (Resp, error) {
	fastCtx := ctxkit.Wrap(ctx)

	tx := AcquireTx(fastCtx)
	defer ReleaseTx(tx)

	p.initTx(tx, pipe)

	startTime := time.Now()

	// 1. Prepare Request
	req = p.handler.Prepare(req, tx)

	// 2. Cache Check
	if tx.Flags&FlagCache != 0 {
		if cachedResp, ok := p.handler.GetCache(req, tx.Cache); ok {
			return cachedResp, nil
		}
	}

	// 3. Tracing
	req, traceInfo, traceEnd := p.handler.TraceRequest(req, tx)

	// 4. Dispatch (Support Custom Phase Order if handler implements it, else generic)
	var (
		resp Resp
		err  error
	)

	resp, err = p.handler.DispatchRequest(req, doer, tx)

	duration := time.Since(startTime)

	if traceEnd != nil {
		traceEnd(resp)
	}

	// 5. Post-Process (Includes AfterResponse hooks)
	resp, err = p.handler.PostProcess(req, resp, tx, err, startTime)

	// 6. Telemetry and HAR
	if tx.Flags&FlagInspect != 0 || tx.Flags&FlagHAR != 0 {
		p.handler.CaptureTelemetry(req, resp, tx, err, traceInfo, startTime, duration)
	}

	return resp, err
}

func (p *Pipeline[Req, Resp]) initTx(tx *Tx, pipe PipelineConfig) {
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

	if pipe.Inspect {
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
		tx.TraceInfo = reqCfg.TraceInfo
		tx.ResponseValidators = reqCfg.ResponseValidators
		tx.SoftErrorDetectors = reqCfg.SoftErrorDetectors
		tx.UnsafePhaseOrder = reqCfg.UnsafePhaseOrder

		if reqCfg.DisabledFlags != 0 {
			flags &^= reqCfg.DisabledFlags
		}

		tx.UnsafeHooks = reqCfg.UnsafeHooks
	}

	tx.Flags = flags
}

// executeCustomPhaseOrder executes the pipeline phases in the custom order defined by the user.
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
				// The hook must be cast from any to func(*Tx, *http.Request, *http.Response) error
				if hookFunc, okFunc := hook.(func(tx *Tx, req *http.Request, resp *http.Response) error); okFunc {
					stdReq, _ := any(req).(*http.Request)

					stdResp, _ := any(resp).(*http.Response)
					if hookErr := hookFunc(tx, stdReq, stdResp); hookErr != nil {
						var zero Resp
						return zero, hookErr
					}
				}
			}
		}

		if phase == PhaseDispatch {
			resp, err = p.handler.DispatchRequest(req, doer, tx)
			if err != nil {
				var zero Resp
				return zero, err
			}
		}
	}

	return resp, nil
}
