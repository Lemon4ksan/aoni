// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/ja4"
)

// TraceInfo records network layer timing metrics for a request.
// Timing fields are fully populated only after the response body is completely read.
type TraceInfo struct {
	// DNSLookup records the time spent resolving the server's IP address.
	DNSLookup time.Duration

	// TCPConn records the time spent establishing the TCP connection.
	TCPConn time.Duration

	// TLSHandshake records the time spent completing the SSL/TLS handshake.
	TLSHandshake time.Duration

	// ServerProcessing records the time from connection establishment to receiving the first response byte.
	ServerProcessing time.Duration

	// ContentTransfer records the time spent transferring the response body data.
	ContentTransfer time.Duration

	// Total records the total execution time for the request.
	Total time.Duration

	// RequestSize records the request payload size in bytes.
	RequestSize int64

	// ResponseSize records the response payload size in bytes.
	ResponseSize int64

	// JA4 holds the JA4 fingerprints computed during the request.
	// Populated only when [TraceJA4] is used as a request modifier.
	JA4 *ja4.Report
}

// Trace returns a [RequestModifier] that registers a connection tracer on the active request.
// Timing metrics are populated inside the provided [TraceInfo] structure.
func Trace(target *TraceInfo) RequestModifier {
	return func(req *http.Request) {
		var dnsStart, connectStart, tlsStart, gotConn time.Time

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

		newReq := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
		*req = *newReq
	}
}

// TraceJA4 returns a [RequestModifier] that populates the JA4 field of the provided [TraceInfo].
// It sets up a shared store in the request context so that [Client.WithTLSFingerprint] can write
// the TLS fingerprint during the handshake, and computes the HTTP fingerprint from request headers.
//
// The JA4 report is fully populated after the request completes. The TLS fingerprint (JA4)
// requires [Client.WithTLSFingerprint] to be enabled.
//
// Use this modifier alongside [Trace] for complete timing and fingerprint data:
//
//	info := &aoni.TraceInfo{}
//	client.Get(ctx, "/path", aoni.Trace(info), aoni.TraceJA4(info))
//	// After request: info.JA4 contains both JA4 and JA4H
func TraceJA4(target *TraceInfo) RequestModifier {
	return func(req *http.Request) {
		// Allocate a store with a pointer to the target TraceInfo.
		// dialTLSWithUTLS will write the TLS report to this store during the handshake.
		// Client.Request will copy it to target after the request completes.
		store := &ja4ReportStore{target: target}
		ctx := context.WithValue(req.Context(), ja4ReportCtxKey{}, store)
		*req = *req.WithContext(ctx)

		// Compute JA4H from request headers (available immediately)
		target.JA4 = &ja4.Report{JA4H: computeJA4HFromRequest(req)}
	}
}

// computeJA4HFromRequest computes a JA4H fingerprint from an http.Request.
func computeJA4HFromRequest(req *http.Request) string {
	method := req.Method
	proto := req.Proto

	// Collect non-Cookie, non-Referer headers
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

	// Sort cookie names and values
	var cookieNames, cookieValues []string
	if hasCookie {
		cookies := req.Cookies()

		type kv struct {
			name  string
			value string
		}

		kvs := make([]kv, len(cookies))
		for i, c := range cookies {
			kvs[i] = kv{c.Name, c.Value}
		}

		// Sort by name
		for i := range kvs {
			for j := i + 1; j < len(kvs); j++ {
				if kvs[j].name < kvs[i].name {
					kvs[i], kvs[j] = kvs[j], kvs[i]
				}
			}
		}

		cookieNames = make([]string, len(kvs))

		cookieValues = make([]string, len(kvs))
		for i, kv := range kvs {
			cookieNames[i] = kv.name
			cookieValues[i] = kv.value
		}
	}

	return ja4.ComputeJA4H(method, proto, headers, hasCookie, hasReferer, acceptLanguage, cookieNames, cookieValues)
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
