// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/ext/telemetry"
)

// StdHandler implements [PipelineHandler] for standard HTTP requests and responses.
type StdHandler struct {
	defaults ClientDefaults
	counter  atomic.Uint32
}

// NewStdHandler constructs a new [StdHandler] with the provided defaults.
func NewStdHandler(defaults ClientDefaults) *StdHandler {
	return &StdHandler{defaults: defaults}
}

// CaptureTelemetry sends the final transaction state to the inspector and HAR tracker.
func (h *StdHandler) CaptureTelemetry(
	req *http.Request,
	resp *http.Response,
	tx *Tx,
	err error,
	traceInfo *telemetry.TraceInfo,
	startTime time.Time,
	duration time.Duration,
) {
	if tx.Flags&FlagInspect != 0 && h.defaults.Inspector != nil {
		h.defaults.Inspector.Capture(req, resp, err, traceInfo)
	}

	if tx.Flags&FlagHAR != 0 && tx.HAR != nil && tx.HAR.Tracker != nil {
		tx.HAR.Tracker.Record(req, resp, startTime, duration.Milliseconds())
	}
}

func (h *StdHandler) enrichError(
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

	var op, urlStr string
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
