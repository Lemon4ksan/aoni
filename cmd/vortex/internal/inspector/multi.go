// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inspector

import (
	"net/http"

	"github.com/lemon4ksan/aoni/telemetry"
)

// MultiInspector wraps multiple [telemetry.TrafficInspector] instances into a multi-broadcaster.
// Every call to Capture is fan-out dispatched to all registered inspectors simultaneously.
type MultiInspector struct {
	inspectors []telemetry.TrafficInspector
}

// NewMultiInspector constructs a [MultiInspector] broadcasting to inspectors.
func NewMultiInspector(inspectors ...telemetry.TrafficInspector) *MultiInspector {
	m := &MultiInspector{}
	m.Add(inspectors...)

	return m
}

// Add registers additional [telemetry.TrafficInspector] targets into the broadcaster.
func (m *MultiInspector) Add(inspectors ...telemetry.TrafficInspector) *MultiInspector {
	for _, insp := range inspectors {
		if insp != nil {
			m.inspectors = append(m.inspectors, insp)
		}
	}

	return m
}

// Capture fan-out broadcasts the execution telemetry event to all registered inspectors.
func (m *MultiInspector) Capture(req *http.Request, resp *http.Response, err error, info *telemetry.TraceInfo) {
	for _, insp := range m.inspectors {
		insp.Capture(req, resp, err, info)
	}
}
