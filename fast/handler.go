// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/pipeline"
	"github.com/lemon4ksan/aoni/telemetry"
)

// FastHandler implements PipelineHandler for the fast engine.
type FastHandler struct {
	defaults pipeline.ClientDefaults
}

func (h *FastHandler) DispatchRequest(
	req aoni.Request,
	doer core.GenericDoer[aoni.Request, aoni.Response],
	tx *pipeline.Tx,
) (aoni.Response, error) {
	return doer.Do(req)
}

func (h *FastHandler) Prepare(req aoni.Request, tx *pipeline.Tx) aoni.Request {
	return req
}

func (h *FastHandler) GetCache(req aoni.Request, cache *pipeline.CacheConfig) (aoni.Response, bool) {
	return nil, false
}

func (h *FastHandler) TraceRequest(
	req aoni.Request,
	tx *pipeline.Tx,
) (aoni.Request, *telemetry.TraceInfo, func(aoni.Response)) {
	return req, nil, nil
}

func (h *FastHandler) PostProcess(
	req aoni.Request,
	resp aoni.Response,
	tx *pipeline.Tx,
	err error,
	startTime time.Time,
) (aoni.Response, error) {
	return resp, err
}

func (h *FastHandler) CaptureTelemetry(
	req aoni.Request,
	resp aoni.Response,
	tx *pipeline.Tx,
	err error,
	traceInfo *telemetry.TraceInfo,
	startTime time.Time,
	duration time.Duration,
) {
}
