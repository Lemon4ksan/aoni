// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"log/slog"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	fastrand "github.com/lemon4ksan/aoni/internal/rand"
	"github.com/lemon4ksan/aoni/netutil/probe"
)

var correlationCounter uint64

// GenerateCorrelationID generates a fast, monotonic Base36 correlation ID string.
func GenerateCorrelationID() string {
	seq := atomic.AddUint64(&correlationCounter, 1)
	timestamp := uint64(time.Now().UnixMicro())*1000 + (uint64(fastrand.Intn(1000)) ^ (seq & 0x3ff))

	var buf [32]byte

	b := strconv.AppendUint(buf[:0], timestamp, 36)

	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}

	return bytesconv.B2S(b)
}

// TraceInfo records network layer execution timings, TLS details, and JA4 signatures for a request.
// TraceInfo records network layer execution timings, TLS details, and JA4 signatures for a request.
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
	IsReused   bool
	JA4        *ja4.Report

	TLSState         *tls.ConnectionState
	CertChain        *probe.CertChainInfo
	Probe            *probe.FullReport
	PeerCertificates []*x509.Certificate

	DNSStart     time.Time
	ConnectStart time.Time
	TLSStart     time.Time
	GotConn      time.Time
}

// EnrichTLSState populates CertChain metadata from the active TLS connection state.
func (t *TraceInfo) EnrichTLSState() {
	if t.TLSState != nil && t.CertChain == nil {
		t.CertChain = probe.InspectTLSChain(t.TLSState)
	}
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

// PeerCertificate returns leaf peer certificate captured during TLS handshakes.
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

// ExtractRedirectHistory extracts intermediate redirect steps from an [*http.Response].
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

// CertSummary holds extracted details for a peer TLS certificate.
type CertSummary struct {
	Subject       string
	Issuer        string
	DNSNames      []string
	NotBefore     time.Time
	NotAfter      time.Time
	SHA256Pin     string
	DaysRemaining int
}

// LogValue implements [slog.LogValuer].
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

// CertSummary extracts structured details for the leaf peer certificate.
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

// Start begins tracking transaction execution timing, returning a completion callback.
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

// ComputeJA4HFromRequest evaluates a JA4H HTTP client fingerprint from an [*http.Request].
func ComputeJA4HFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}

	headers := make([]string, 0, len(req.Header))
	hasCookie := false
	hasReferer := false
	acceptLanguage := ""

	for name := range req.Header {
		switch {
		case bytesconv.EqualFoldASCII(name, "cookie"):
			hasCookie = true
		case bytesconv.EqualFoldASCII(name, "referer"):
			hasReferer = true
		case bytesconv.EqualFoldASCII(name, "accept-language"):
			acceptLanguage = req.Header.Get(name)
		default:
			headers = append(headers, name)
		}
	}

	var cookieNames, cookieValues []string
	if hasCookie {
		cookies := req.Cookies()

		type kv struct {
			name  string
			value string
		}

		kvs := make([]kv, len(cookies))
		for i, c := range cookies {
			kvs[i] = kv{name: c.Name, value: c.Value}
		}

		slices.SortFunc(kvs, func(a, b kv) int {
			return strings.Compare(a.name, b.name)
		})

		cookieNames = make([]string, len(kvs))
		cookieValues = make([]string, len(kvs))

		for i, item := range kvs {
			cookieNames[i] = item.name
			cookieValues[i] = item.value
		}
	}

	return ja4.ComputeJA4H(
		req.Method,
		req.Proto,
		headers,
		hasCookie,
		hasReferer,
		acceptLanguage,
		cookieNames,
		cookieValues,
	)
}

// TriggerGot1xxResponse notifies active httptrace ClientTrace hooks of intermediate 1xx responses (100, 102, 103).
//
// Postconditions:
//   - If the active Got1xxResponse callback returns a non-nil error, the request execution MUST be aborted immediately.
func TriggerGot1xxResponse(ctx context.Context, code int, header http.Header) error {
	trace := httptrace.ContextClientTrace(ctx)
	if trace == nil || trace.Got1xxResponse == nil {
		return nil
	}

	mimeHeader := make(textproto.MIMEHeader, len(header))
	maps.Copy(mimeHeader, header)

	return trace.Got1xxResponse(code, mimeHeader)
}

// TruncateBody limits output payload representations to maxBytes without unnecessary allocations.
func TruncateBody(body []byte, maxBytes int) string {
	limit := maxBytes
	if limit <= 0 {
		limit = 4096
	}

	if len(body) <= limit {
		return bytesconv.B2S(body)
	}

	var numBuf [20]byte

	truncatedBytes := strconv.AppendInt(numBuf[:0], int64(len(body)-limit), 10)

	var sb strings.Builder
	sb.Grow(limit + 32 + len(truncatedBytes))
	sb.WriteString(bytesconv.B2S(body[:limit]))
	sb.WriteString("... [truncated ")
	sb.Write(truncatedBytes)
	sb.WriteString(" bytes]")

	return sb.String()
}

// IsStreamingResponse detects whether an HTTP response represents a real-time stream (SSE, NDJSON, Chunked).
func IsStreamingResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	streamingTypes := [...]string{
		"text/event-stream",
		"application/stream",
		"application/x-ndjson",
		"application/x-stream",
		"text/stream",
	}

	for _, st := range streamingTypes {
		if strings.Contains(contentType, st) {
			return true
		}
	}

	chunked := strings.Contains(strings.ToLower(resp.Header.Get("Transfer-Encoding")), "chunked")

	return strings.Contains(contentType, "text/plain") && chunked && resp.ContentLength == -1
}

// SummarizeMultipartBody extracts form field names and file metadata from a multipart payload.
func SummarizeMultipartBody(body []byte, contentType string) string {
	if len(body) == 0 || contentType == "" {
		return ""
	}

	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return "(multipart/form-data payload)"
	}

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])

	var parts []string

	for {
		part, err := reader.NextPart()
		if err == io.EOF || err != nil {
			break
		}

		name := part.FormName()
		if name == "" {
			continue
		}

		if filename := part.FileName(); filename != "" {
			parts = append(parts, name+"=@"+filename)
			continue
		}

		var sb strings.Builder
		if _, err := io.Copy(&sb, part); err == nil {
			parts = append(parts, name+"="+sb.String())
		}
	}

	if len(parts) == 0 {
		return "(multipart/form-data payload)"
	}

	return strings.Join(parts, "&")
}
