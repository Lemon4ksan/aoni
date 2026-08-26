// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/net/headkit"
	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/timekit"

	"github.com/lemon4ksan/aoni/internal/version"
)

// HARGenerator captures and aggregates HTTP request-response sessions into HAR 1.2 JSON format.
//
// Specification Adherence:
// Conforms strictly to W3C HTTP Archive (HAR) 1.2 specification format.
//
// Thread Safety & Concurrency:
// 100% thread-safe; guarded by internal read-write mutex lock (`sync.RWMutex`).
type HARGenerator struct {
	mu      sync.RWMutex
	entries []HAREntry
}

// NewHARGenerator instantiates an empty, thread-safe [HARGenerator] ready for recording.
//
// Postconditions:
//   - Yields a non-nil [HARGenerator] pointer with initialized slice storage.
func NewHARGenerator() *HARGenerator {
	return &HARGenerator{
		entries: make([]HAREntry, 0),
	}
}

// Record captures details of a completed request-response transaction into a structured [HAREntry].
//
// Preconditions:
//   - If resp is nil, the invocation is safely ignored without state mutation.
//
// Postconditions:
//   - Appends a newly created [HAREntry] to internal thread-safe slice storage.
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
	for k, val := range headkit.Flatten(req.Header) {
		reqHeaders = append(reqHeaders, HARHeaderField{Name: k, Value: val})
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
			Expires:  timekit.FormatRFC3339(c.Expires),
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
		})
	}

	respHeaders := make([]HARHeaderField, 0, len(resp.Header))
	for k, val := range headkit.Flatten(resp.Header) {
		respHeaders = append(respHeaders, HARHeaderField{Name: k, Value: val})
	}

	respCookies := make([]HARCookieField, 0, len(resp.Cookies()))
	for _, c := range resp.Cookies() {
		respCookies = append(respCookies, HARCookieField{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  timekit.FormatRFC3339(c.Expires),
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
		})
	}

	bodyBytes := captureHARResponseBody(resp)

	g.AddEntry(HAREntry{
		StartedDateTime: timekit.FormatRFC3339(startTime),
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
				MimeType: resp.Header.Get(fheader.ContentType),
				Text:     string(bodyBytes),
			},
			RedirectURL: resp.Header.Get(fheader.Location),
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

type multiReadCloser struct {
	io.Reader
	io.Closer
}

// captureHARResponseBody buffers up to 150 KB of textual response body payload for HAR export
// without mutating or closing the underlying caller response stream.
func captureHARResponseBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil || resp.Body == http.NoBody {
		return nil
	}

	contentType := resp.Header.Get(fheader.ContentType)
	isText := strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "text") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "javascript")

	if !isText {
		return []byte("[Skipped: Binary response body]")
	}

	if resp.ContentLength != -1 && resp.ContentLength > 150*1024 {
		return []byte("[Skipped: Large response body]")
	}

	const maxCap = 150 * 1024

	limitReader := io.LimitReader(resp.Body, maxCap+1)
	readBytes, _ := io.ReadAll(limitReader)

	isTruncated := len(readBytes) > maxCap

	var logBytes []byte

	if isTruncated {
		logBytes = []byte("[Truncated: Response too large for HAR log]")
	} else {
		logBytes = readBytes
	}

	// Restore original stream seamlessly so consumer reads full unmodified body
	resp.Body = &multiReadCloser{
		Reader: io.MultiReader(bytes.NewReader(readBytes), resp.Body),
		Closer: resp.Body,
	}

	return logBytes
}

// AddEntry adds a single [HAREntry] to the generator thread-safely.
func (g *HARGenerator) AddEntry(entry HAREntry) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.entries = append(g.entries, entry)
}

// Export serializes logged entries into HAR 1.2 compliant JSON bytes.
func (g *HARGenerator) Export() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	entries := g.entries
	if entries == nil {
		entries = make([]HAREntry, 0)
	}

	logData := HARLog{
		Log: HARLogDetail{
			Version: "1.2",
			Creator: HARCreator{
				Name:    "aoni",
				Version: version.Number,
			},
			Entries: entries,
		},
	}

	return json.MarshalIndent(logData, "", "  ")
}

// HARLog represents top-level HAR structure.
type HARLog struct {
	Log HARLogDetail `json:"log"`
}

// HARLogDetail holds metadata and entry lists.
type HARLogDetail struct {
	Version string     `json:"version"`
	Creator HARCreator `json:"creator"`
	Entries []HAREntry `json:"entries"`
}

// HARCreator holds generator application metadata.
type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HAREntry represents a captured request-response transaction.
type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Cache           any         `json:"cache"`
	Timings         HARTimings  `json:"timings"`
}

// HARRequest represents a captured HTTP request.
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

// HARResponse represents a captured HTTP response.
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

// HARHeaderField represents an HTTP header name-value pair.
type HARHeaderField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARCookieField represents a cookie in HAR format.
type HARCookieField struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path"`
	Domain   string `json:"domain"`
	Expires  string `json:"expires"`
	HTTPOnly bool   `json:"httpOnly"`
	Secure   bool   `json:"secure"`
}

// HARQueryField represents a URL query parameter.
type HARQueryField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARContent represents response body details.
type HARContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

// HARTimings represents timing metrics in milliseconds.
type HARTimings struct {
	Send    int64 `json:"send"`
	Wait    int64 `json:"wait"`
	Receive int64 `json:"receive"`
}
