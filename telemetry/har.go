// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package telemetry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HARGenerator thread-safely captures and aggregates HTTP request-response
// transactions into a standard-compliant HTTP Archive (HAR) format.
type HARGenerator struct {
	mu      sync.RWMutex
	entries []HAREntry
}

// NewHARGenerator instantiates an empty [HARGenerator] ready to record sessions.
func NewHARGenerator() *HARGenerator {
	return &HARGenerator{
		entries: make([]HAREntry, 0),
	}
}

// Record compiles the transaction detail of a completed request and response cycle
// into a structured HAR entry.
//
// Preconditions:
//   - The response argument must not be nil.
//
// Side effects:
//   - If the response is text-based and its size is under 150 KB, the body is read,
//     buffered, and transparently replaced with a fresh [io.ReadCloser] to ensure
//     subsequent readers can still consume the response body stream cleanly.
//   - If the response body is binary or exceeds 150 KB, body capture is skipped
//     defensively to prevent memory exhaustion (OOM).
func (g *HARGenerator) Record(
	req *http.Request,
	resp *http.Response,
	startTime time.Time,
	duration int64,
) {
	if resp == nil {
		return
	}

	reqHeaders := make([]HARHeaderField, 0, len(req.Header))
	for k, v := range req.Header {
		for _, val := range v {
			reqHeaders = append(reqHeaders, HARHeaderField{Name: k, Value: val})
		}
	}

	var reqBodySize int64
	if req.Body != nil && req.Body != http.NoBody && req.ContentLength > 0 {
		reqBodySize = req.ContentLength
	}

	reqQuery := make([]HARQueryField, 0, len(req.URL.Query()))
	for k, v := range req.URL.Query() {
		for _, val := range v {
			reqQuery = append(reqQuery, HARQueryField{Name: k, Value: val})
		}
	}

	reqCookies := make([]HARCookieField, 0, len(req.Cookies()))
	for _, c := range req.Cookies() {
		reqCookies = append(reqCookies, HARCookieField{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  c.Expires.Format(time.RFC3339Nano),
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
		})
	}

	respHeaders := make([]HARHeaderField, 0, len(resp.Header))
	for k, v := range resp.Header {
		for _, val := range v {
			respHeaders = append(respHeaders, HARHeaderField{Name: k, Value: val})
		}
	}

	respCookies := make([]HARCookieField, 0, len(resp.Cookies()))
	for _, c := range resp.Cookies() {
		respCookies = append(respCookies, HARCookieField{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  c.Expires.Format(time.RFC3339Nano),
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
		})
	}

	var bodyBytes []byte
	if resp.Body != nil {
		contentType := resp.Header.Get("Content-Type")
		isText := strings.Contains(contentType, "json") ||
			strings.Contains(contentType, "text") ||
			strings.Contains(contentType, "xml")

		if isText && (resp.ContentLength == -1 || resp.ContentLength < 150*1024) {
			limitReader := io.LimitReader(resp.Body, 150*1024+1)
			bodyBytes, _ = io.ReadAll(limitReader)
			_ = resp.Body.Close()

			if int64(len(bodyBytes)) > 150*1024 {
				bodyBytes = []byte("[Truncated: Response too large for HAR log]")
			}

			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		} else {
			bodyBytes = []byte("[Skipped: Binary or large response body]")
		}
	}

	g.AddEntry(HAREntry{
		StartedDateTime: startTime.UTC().Format(time.RFC3339Nano),
		Time:            duration,
		Request: HARRequest{
			Method:      req.Method,
			URL:         req.URL.String(),
			HTTPVersion: req.Proto,
			Headers:     reqHeaders,
			Cookies:     reqCookies,
			QueryString: reqQuery,
			HeadersSize: -1,
			BodySize:    reqBodySize,
		},
		Response: HARResponse{
			Status:      resp.StatusCode,
			StatusText:  resp.Status,
			HTTPVersion: resp.Proto,
			Headers:     respHeaders,
			Cookies:     respCookies,
			Content: HARContent{
				Size:     int64(len(bodyBytes)),
				MimeType: resp.Header.Get("Content-Type"),
				Text:     string(bodyBytes),
			},
			RedirectURL: resp.Header.Get("Location"),
			HeadersSize: -1,
			BodySize:    int64(len(bodyBytes)),
		},
		Cache: struct{}{},
		Timings: HARTimings{
			Send:    0,
			Wait:    duration,
			Receive: 0,
		},
	})
}

// AddEntry adds a single HAREntry thread-safely.
func (g *HARGenerator) AddEntry(entry HAREntry) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.entries = append(g.entries, entry)
}

// Export serializes the logged entries into standard HAR JSON format.
func (g *HARGenerator) Export() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	entries := g.entries
	if entries == nil {
		entries = make([]HAREntry, 0)
	}

	log := HARLog{
		Log: HARLogDetail{
			Version: "1.2",
			Creator: HARCreator{
				Name:    "aoni",
				Version: "0.5.0",
			},
			Entries: entries,
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
	Cookies     []HARCookieField `json:"cookies"`
	QueryString []HARQueryField  `json:"queryString"`
	HeadersSize int              `json:"headersSize"`
	BodySize    int64            `json:"bodySize"`
}

// HARResponse represents a captured HTTP response in the HAR log.
type HARResponse struct {
	Status      int              `json:"status"`
	StatusText  string           `json:"statusText"`
	HTTPVersion string           `json:"httpVersion"`
	Headers     []HARHeaderField `json:"headers"`
	Cookies     []HARCookieField `json:"cookies"`
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

// HARCookieField represents a cookie in the HAR log.
type HARCookieField struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path"`
	Domain   string `json:"domain"`
	Expires  string `json:"expires"`
	HTTPOnly bool   `json:"httpOnly"`
	Secure   bool   `json:"secure"`
}

// HARQueryField represents an URL query parameter in the HAR log.
type HARQueryField struct {
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
