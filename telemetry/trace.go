// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package telemetry provides observability, diagnostic logging, and latency tracking utilities.
package telemetry

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
)

var correlationCounter uint64

// GenerateCorrelationID generates a fast, unique 16-character hex correlation identifier for request tracing.
func GenerateCorrelationID() string {
	id := atomic.AddUint64(&correlationCounter, 1)
	return fmt.Sprintf("%016x", id)
}

// TraceInfo records network layer execution timings, metrics, and TLS/HTTP fingerprints for a request.
// Detailed timing fields are populated progressively as execution phases complete.
type TraceInfo struct {
	CorrelationID    string
	Label            string
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

	TLSState         *tls.ConnectionState
	PeerCertificates []*x509.Certificate

	DNSStart     time.Time
	ConnectStart time.Time
	TLSStart     time.Time
	GotConn      time.Time
}

// LogValue implements [slog.LogValuer] for structured logging with log/slog.
func (t *TraceInfo) LogValue() slog.Value {
	if t == nil {
		return slog.Value{}
	}

	attrs := []slog.Attr{
		slog.String("correlation_id", t.CorrelationID),
		slog.String("remote_addr", t.RemoteAddr),
		slog.Duration("dns_lookup", t.DNSLookup),
		slog.Duration("tcp_conn", t.TCPConn),
		slog.Duration("tls_handshake", t.TLSHandshake),
		slog.Duration("server_processing", t.ServerProcessing),
		slog.Duration("total", t.Total),
	}

	if t.Label != "" {
		attrs = append(attrs, slog.String("label", t.Label))
	}

	if t.JA4 != nil {
		attrs = append(attrs, slog.String("ja4", t.JA4.JA4))
	}

	if cert := t.CertSummary(); cert != nil {
		attrs = append(attrs, slog.Any("cert", cert))
	}

	return slog.GroupValue(attrs...)
}

// PeerCertificate returns the leaf server certificate captured during the TLS handshake.
func (t *TraceInfo) PeerCertificate() *x509.Certificate {
	if len(t.PeerCertificates) > 0 {
		return t.PeerCertificates[0]
	}

	return nil
}

// RedirectHop represents a single intermediate HTTP redirect step.
type RedirectHop struct {
	URL        string
	StatusCode int
	Method     string
}

// ExtractRedirectHistory traverses the http.Response.Request.Response chain
// and returns all intermediate redirect hops in chronological order.
func ExtractRedirectHistory(resp *http.Response) []RedirectHop {
	if resp == nil || resp.Request == nil || resp.Request.Response == nil {
		return nil
	}

	var hops []RedirectHop

	curr := resp.Request.Response

	for curr != nil {
		reqURL := ""

		method := ""
		if curr.Request != nil {
			if curr.Request.URL != nil {
				reqURL = curr.Request.URL.String()
			}

			method = curr.Request.Method
		}

		hops = append(hops, RedirectHop{
			URL:        reqURL,
			StatusCode: curr.StatusCode,
			Method:     method,
		})

		if curr.Request != nil {
			curr = curr.Request.Response
		} else {
			curr = nil
		}
	}

	slices.Reverse(hops)

	return hops
}

// CertSummary holds extracted information about the server's TLS certificate.
type CertSummary struct {
	Subject       string
	Issuer        string
	DNSNames      []string
	NotBefore     time.Time
	NotAfter      time.Time
	SHA256Pin     string
	DaysRemaining int
}

// LogValue implements [slog.LogValuer] for structured logging with log/slog.
func (c *CertSummary) LogValue() slog.Value {
	if c == nil {
		return slog.Value{}
	}

	return slog.GroupValue(
		slog.String("issuer", c.Issuer),
		slog.String("subject", c.Subject),
		slog.Int("days_remaining", c.DaysRemaining),
		slog.String("sha256_pin", c.SHA256Pin),
	)
}

// TruncateBody safely limits body payload output length to maxBytes to prevent console/log spam.
func TruncateBody(body []byte, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 4096 // Default 4 KB debug limit
	}

	if len(body) <= maxBytes {
		return string(body)
	}

	return fmt.Sprintf("%s... [truncated %d bytes]", string(body[:maxBytes]), len(body)-maxBytes)
}

// CertSummary extracts and returns structured details for the peer certificate.
func (t *TraceInfo) CertSummary() *CertSummary {
	cert := t.PeerCertificate()
	if cert == nil {
		return nil
	}

	hash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	pin := hex.EncodeToString(hash[:])

	days := int(time.Until(cert.NotAfter).Hours() / 24)

	return &CertSummary{
		Subject:       cert.Subject.String(),
		Issuer:        cert.Issuer.String(),
		DNSNames:      cert.DNSNames,
		NotBefore:     cert.NotBefore,
		NotAfter:      cert.NotAfter,
		SHA256Pin:     pin,
		DaysRemaining: days,
	}
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
