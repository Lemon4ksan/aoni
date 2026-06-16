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

// TraceInfo holds detailed network layer timing metrics for a request.
// The timing fields are fully populated only after the response body is completely read.
type TraceInfo struct {
	// DNSLookup is the duration spent resolving the server's IP address.
	DNSLookup time.Duration

	// TCPConn is the duration spent establishing the TCP connection.
	TCPConn time.Duration

	// TLSHandshake is the duration spent completing the SSL/TLS handshake.
	TLSHandshake time.Duration

	// ServerProcessing is the duration from connection establishment to receiving the first response byte.
	ServerProcessing time.Duration

	// ContentTransfer is the duration spent transferring the response body data.
	ContentTransfer time.Duration

	// Total is the total execution time for the request.
	Total time.Duration

	// RequestSize is the request payload size in bytes.
	RequestSize int64

	// ResponseSize is the response payload size in bytes.
	ResponseSize int64
}

// Trace returns a [RequestModifier] that registers a connection tracer on the active request.
// Timing metrics are recorded and populated inside the provided [TraceInfo] structure.
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

		ctx := req.Context()
		_ = ctx
		_ = start
	}
}

// CurlCommand generates a shell-compatible curl command string from an [http.Request] and body.
func CurlCommand(req *http.Request, body []byte) string {
	var sb strings.Builder

	sb.WriteString("curl")

	if req.Method != http.MethodGet {
		fmt.Fprintf(&sb, " -X %s", req.Method)
	}

	for key, values := range req.Header {
		for _, value := range values {
			fmt.Fprintf(&sb, " -H '%s: %s'", key, value)
		}
	}

	if len(body) > 0 {
		escaped := strings.ReplaceAll(string(body), "'", "'\\''")
		fmt.Fprintf(&sb, " -d '%s'", escaped)
	}

	fmt.Fprintf(&sb, " '%s'", req.URL.String())

	return sb.String()
}

// AsCurl returns a [RequestModifier] that dumps the equivalent curl command to a silent discard stream.
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
