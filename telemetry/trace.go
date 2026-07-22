// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package telemetry provides observability, diagnostic logging, and latency tracking utilities.
package telemetry

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
)

// TraceInfo records network layer execution timings, metrics, and TLS/HTTP fingerprints for a request.
// Detailed timing fields are populated progressively as execution phases complete.
type TraceInfo struct {
	DNSLookup        time.Duration
	TCPConn          time.Duration
	TLSHandshake     time.Duration
	ServerProcessing time.Duration
	ContentTransfer  time.Duration
	Total            time.Duration

	RequestSize  int64
	ResponseSize int64

	RemoteAddr string
	JA4        *ja4.Report

	DNSStart     time.Time
	ConnectStart time.Time
	TLSStart     time.Time
	GotConn      time.Time
}

// Start begins tracking total transaction and content transfer timing.
// Returns a completion callback to be invoked when the response body is fully read.
func (t *TraceInfo) Start() func(resp *http.Response) {
	start := time.Now()

	return func(resp *http.Response) {
		t.Total = time.Since(start)

		if resp == nil {
			return
		}

		if resp.ContentLength > 0 {
			t.ResponseSize = resp.ContentLength
		}

		if t.ServerProcessing > 0 {
			setupTime := t.DNSLookup + t.TCPConn + t.TLSHandshake + t.ServerProcessing
			if t.Total > setupTime {
				t.ContentTransfer = t.Total - setupTime
			}
		}
	}
}

// ComputeJA4HFromRequest computes a JA4H HTTP client fingerprint directly from an [http.Request].
func ComputeJA4HFromRequest(req *http.Request) string {
	method := req.Method
	proto := req.Proto

	var headers []string

	hasCookie := false
	hasReferer := false
	acceptLanguage := ""

	for name := range req.Header {
		switch strings.ToLower(name) {
		case "cookie":
			hasCookie = true
		case "referer":
			hasReferer = true
		case "accept-language":
			acceptLanguage = req.Header.Get(name)
		default:
			headers = append(headers, name)
		}
	}

	var cookieNames, cookieValues []string
	if hasCookie {
		type kv struct {
			name  string
			value string
		}

		kvs := generic.Map(req.Cookies(), func(c *http.Cookie) kv {
			return kv{name: c.Name, value: c.Value}
		})

		slices.SortFunc(kvs, func(a, b kv) int {
			return strings.Compare(a.name, b.name)
		})

		cookieNames = generic.Map(kvs, func(k kv) string { return k.name })
		cookieValues = generic.Map(kvs, func(k kv) string { return k.value })
	}

	return ja4.ComputeJA4H(method, proto, headers, hasCookie, hasReferer, acceptLanguage, cookieNames, cookieValues)
}

// CurlFromRequest builds a shell-compatible curl command string from an [http.Request] and body payload.
func CurlFromRequest(req *http.Request, body []byte) string {
	var sb strings.Builder

	sb.WriteString("curl")

	if req.Method != http.MethodGet {
		fmt.Fprintf(&sb, " -X %s", req.Method)
	}

	for key, values := range req.Header {
		for _, value := range values {
			escapedKey := strings.ReplaceAll(key, "'", "'\\''")
			escapedVal := strings.ReplaceAll(value, "'", "'\\''")
			fmt.Fprintf(&sb, " -H '%s: %s'", escapedKey, escapedVal)
		}
	}

	if len(body) > 0 {
		escaped := strings.ReplaceAll(string(body), "'", "'\\''")
		fmt.Fprintf(&sb, " -d '%s'", escaped)
	}

	escapedURL := strings.ReplaceAll(req.URL.String(), "'", "'\\''")
	fmt.Fprintf(&sb, " '%s'", escapedURL)

	return sb.String()
}
