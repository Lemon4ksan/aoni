// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"encoding/json"
	"sync"
)

// HARGenerator accumulates HTTP request-response entries.
type HARGenerator struct {
	mu      sync.RWMutex
	entries []HAREntry
}

// NewHARGenerator creates a new HARGenerator.
func NewHARGenerator() *HARGenerator {
	return &HARGenerator{}
}

// AddEntry adds a single HAREntry thread-safely.
func (g *HARGenerator) AddEntry(entry HAREntry) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.entries = append(g.entries, entry)
}

// Export returns the serialized HAR archive JSON bytes.
func (g *HARGenerator) Export() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	log := HARLog{
		Log: HARLogDetail{
			Version: "1.2",
			Creator: HARCreator{
				Name:    "aoni",
				Version: "0.5.0",
			},
			Entries: g.entries,
		},
	}

	return json.MarshalIndent(log, "", "  ")
}

// HARLog represents the top-level HAR log structure.
type HARLog struct {
	Log HARLogDetail `json:"log"`
}

// HARLogDetail represents the log details in a HAR file.
type HARLogDetail struct {
	Version string     `json:"version"`
	Creator HARCreator `json:"creator"`
	Entries []HAREntry `json:"entries"`
}

// HARCreator represents the creator of the HAR file.
type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HAREntry represents a single request-response session entry in the HAR log.
type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Cache           any         `json:"cache"`
	Timings         HARTimings  `json:"timings"`
}

// HARRequest represents a captured HTTP request in the HAR log.
type HARRequest struct {
	Method      string           `json:"method"`
	URL         string           `json:"url"`
	HTTPVersion string           `json:"httpVersion"`
	Headers     []HARHeaderField `json:"headers"`
	Cookies     []any            `json:"cookies"`
	QueryString []any            `json:"queryString"`
	HeadersSize int              `json:"headersSize"`
	BodySize    int64            `json:"bodySize"`
}

// HARResponse represents a captured HTTP response in the HAR log.
type HARResponse struct {
	Status      int              `json:"status"`
	StatusText  string           `json:"statusText"`
	HTTPVersion string           `json:"httpVersion"`
	Headers     []HARHeaderField `json:"headers"`
	Cookies     []any            `json:"cookies"`
	Content     HARContent       `json:"content"`
	RedirectURL string           `json:"redirectURL"`
	HeadersSize int              `json:"headersSize"`
	BodySize    int64            `json:"bodySize"`
}

// HARHeaderField represents an HTTP header name-value pair in the HAR log.
type HARHeaderField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARContent represents the response body content details in the HAR log.
type HARContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

// HARTimings represents the timing metrics for a request-response session in the HAR log.
type HARTimings struct {
	Send    int64 `json:"send"`
	Wait    int64 `json:"wait"`
	Receive int64 `json:"receive"`
}
