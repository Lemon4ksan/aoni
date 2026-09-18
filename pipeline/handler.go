// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"time"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/x/telemetry"
)

// PipelineHandler defines the domain-specific logic for processing a transaction.
// It is used by [Pipeline] to orchestrate the lifecycle without being coupled to specific request/response types.
type PipelineHandler[Req, Resp any] interface {
	// DispatchRequest executes the request using the doer, handling retries and hedging.
	DispatchRequest(req Req, doer core.GenericDoer[Req, Resp], tx *Tx) (Resp, error)

	// Prepare applies pre-flight modifications (e.g., headers, URIs) to the request.
	Prepare(req Req, tx *Tx) Req

	// GetCache attempts to return a cached response.
	GetCache(req Req, cache *CacheConfig) (Resp, bool)

	// PostProcess handles the response (e.g., decompression, caching) after it's received.
	PostProcess(req Req, resp Resp, tx *Tx, err error, startTime time.Time) (Resp, error)

	// TraceRequest initiates telemetry tracing for the request.
	TraceRequest(req Req, tx *Tx) (Req, *telemetry.TraceInfo, func(Resp))

	// CaptureTelemetry sends the final transaction state to the inspector and HAR tracker.
	CaptureTelemetry(
		req Req,
		resp Resp,
		tx *Tx,
		err error,
		traceInfo *telemetry.TraceInfo,
		startTime time.Time,
		duration time.Duration,
	)
}
