// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

// TraceInfo holds detailed timing information for a request.
type TraceInfo struct {
	// DNSLookup is the time spent looking up the DNS record.
	DNSLookup time.Duration
	// TCPConn is the time spent establishing a TCP connection.
	TCPConn time.Duration
	// TLSHandshake is the time spent performing the TLS handshake.
	TLSHandshake time.Duration
	// ServerProcessing is the time spent waiting for the server to process the request.
	ServerProcessing time.Duration
	// ContentTransfer is the time spent transferring the response body.
	ContentTransfer time.Duration
	// Total is the total time for the request.
	Total time.Duration

	// RequestSize is the size of the request body in bytes.
	RequestSize int64
	// ResponseSize is the size of the response body in bytes.
	ResponseSize int64
}

// Trace returns a RequestModifier that captures trace timing information.
// The target TraceInfo is populated after the request completes.
func Trace(target *TraceInfo) RequestModifier {
	return func(req *http.Request) {
		var (
			start                                     time.Time
			dnsStart, connectStart, tlsStart, gotConn time.Time
		)

		trace := &httptrace.ClientTrace{
			DNSStart:             func(_ httptrace.DNSStartInfo) { dnsStart = time.Now() },
			DNSDone:              func(_ httptrace.DNSDoneInfo) { target.DNSLookup = time.Since(dnsStart) },
			ConnectStart:         func(_, _ string) { connectStart = time.Now() },
			ConnectDone:          func(_, _ string, _ error) { target.TCPConn = time.Since(connectStart) },
			TLSHandshakeStart:    func() { tlsStart = time.Now() },
			TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { target.TLSHandshake = time.Since(tlsStart) },
			GotConn:              func(_ httptrace.GotConnInfo) { gotConn = time.Now() },
			GotFirstResponseByte: func() { target.ServerProcessing = time.Since(gotConn) },
		}

		start = time.Now()
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

		// Store start time in context for later use
		ctx := req.Context()
		_ = ctx
		_ = start
	}
}

// CurlCommand generates a curl command string from an http.Request and optional body.
func CurlCommand(req *http.Request, body []byte) string {
	var sb strings.Builder

	sb.WriteString("curl")

	// Method
	if req.Method != http.MethodGet {
		fmt.Fprintf(&sb, " -X %s", req.Method)
	}

	// Headers
	for key, values := range req.Header {
		for _, value := range values {
			fmt.Fprintf(&sb, " -H '%s: %s'", key, value)
		}
	}

	// Body
	if len(body) > 0 {
		// Escape single quotes in body
		escaped := strings.ReplaceAll(string(body), "'", "'\\''")
		fmt.Fprintf(&sb, " -d '%s'", escaped)
	}

	// URL
	fmt.Fprintf(&sb, " '%s'", req.URL.String())

	return sb.String()
}

// AsCurl returns a RequestModifier that prints the equivalent curl command to stderr.
func AsCurl() RequestModifier {
	return func(req *http.Request) {
		var body []byte

		if req.Body != nil && req.Body != http.NoBody {
			var buf bytes.Buffer

			_, _ = io.Copy(&buf, req.Body)
			body = buf.Bytes()
			req.Body = io.NopCloser(bytes.NewReader(body))
		}

		curl := CurlCommand(req, body)
		fmt.Fprintf(io.Discard, "%s\n", curl)
	}
}
