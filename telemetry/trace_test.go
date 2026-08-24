// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/telemetry"
)

func SetupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *aoni.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	return server, client
}

func TestTraceInfo_Start_CalculatesTransfer(t *testing.T) {
	t.Parallel()

	info := &telemetry.TraceInfo{
		DNSLookup:        5 * time.Millisecond,
		TCPConn:          10 * time.Millisecond,
		TLSHandshake:     15 * time.Millisecond,
		ServerProcessing: 20 * time.Millisecond,
	}

	finish := info.Start()

	time.Sleep(60 * time.Millisecond) // Simulate body read delay

	resp := &http.Response{
		ContentLength: 1024,
	}

	finish(resp)

	assert.Greater(t, info.Total, 50*time.Millisecond)
	assert.Equal(t, int64(1024), info.ResponseSize)
	assert.Greater(t, info.ContentTransfer, time.Duration(0))
}

func TestTraceInfo_CertSummary(t *testing.T) {
	t.Parallel()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"Aoni Issuer Org"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{Organization: []string{"Aoni Test Org"}},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(10 * 24 * time.Hour),
		DNSNames:     []string{"example.com"},
	}

	leafCertDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	leafCert, err := x509.ParseCertificate(leafCertDER)
	require.NoError(t, err)

	info := &telemetry.TraceInfo{
		PeerCertificates: []*x509.Certificate{leafCert},
	}

	summary := info.CertSummary()
	require.NotNil(t, summary)

	assert.Contains(t, summary.Subject, "Aoni Test Org")
	assert.Contains(t, summary.Issuer, "Aoni Issuer Org")
	assert.Contains(t, summary.DNSNames, "example.com")
	assert.GreaterOrEqual(t, summary.DaysRemaining, 9)

	hash := sha256.Sum256(leafCert.RawSubjectPublicKeyInfo)
	expectedPin := hex.EncodeToString(hash[:])
	assert.Equal(t, expectedPin, summary.SHA256Pin)

	slogVal := summary.LogValue()
	assert.Equal(t, slog.KindGroup, slogVal.Kind())
}

func TestTraceJA4(t *testing.T) {
	t.Parallel()

	t.Run("generate_ja4h_fingerprint", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/api", nil)
		require.NoError(t, err)

		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Accept-Language", "en-US")
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

		var traceInfo telemetry.TraceInfo

		m := mod.WithTraceJA4(&traceInfo)
		m.ApplyStd(req)

		require.NotNil(t, traceInfo.JA4)
		assert.NotEmpty(t, traceInfo.JA4.JA4H)
		assert.Contains(t, traceInfo.JA4.JA4H, "po11")
	})
}

func TestTriggerGot1xxResponse(t *testing.T) {
	t.Parallel()

	var (
		receivedCode   int
		receivedHeader string
	)

	trace := &httptrace.ClientTrace{
		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			receivedCode = code
			receivedHeader = header.Get("Link")
			return nil
		},
	}

	ctx := httptrace.WithClientTrace(t.Context(), trace)

	header := http.Header{}
	header.Set("Link", "</style.css>; rel=preload")

	err := telemetry.TriggerGot1xxResponse(ctx, 103, header)
	require.NoError(t, err)

	assert.Equal(t, 103, receivedCode)
	assert.Equal(t, "</style.css>; rel=preload", receivedHeader)
}

func TestCurlCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  string
		url     string
		headers map[string]string
		body    []byte
		want    []string
		notWant []string
	}{
		{
			name:    "simple_get_request",
			method:  "GET",
			url:     "http://example.com/api/test",
			headers: map[string]string{"Authorization": "Bearer token123"},
			body:    nil,
			want:    []string{"curl", "http://example.com/api/test", "*****REDACTED*****"},
		},
		{
			name:    "post_request_with_body",
			method:  "POST",
			url:     "http://example.com/api/test",
			headers: map[string]string{"Content-Type": "application/json"},
			body:    []byte(`{"key": "value"}`),
			want:    []string{"-X POST", "-d '{\"key\": \"value\"}'", "Content-Type: application/json"},
		},
		{
			name:    "get_request_no_method_flag",
			method:  "GET",
			url:     "http://example.com/api/test",
			headers: nil,
			body:    nil,
			want:    []string{"http://example.com/api/test"},
			notWant: []string{"-X GET"},
		},
		{
			name:    "request_with_multiple_headers",
			method:  "GET",
			url:     "http://example.com/api/test",
			headers: map[string]string{"X-Custom1": "value1", "X-Custom2": "value2"},
			body:    nil,
			want:    []string{"X-Custom1: value1", "X-Custom2: value2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), tt.method, tt.url, nil)
			require.NoError(t, err)

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			curl := telemetry.CurlFromRequest(req, tt.body)
			for _, w := range tt.want {
				assert.Contains(t, curl, w)
			}

			for _, nw := range tt.notWant {
				assert.NotContains(t, curl, nw)
			}
		})
	}
}

func TestCorrelationID_And_Slog_And_TruncateBody(t *testing.T) {
	t.Parallel()

	cid1 := telemetry.GenerateCorrelationID()
	cid2 := telemetry.GenerateCorrelationID()

	assert.NotEmpty(t, cid1)
	assert.NotEmpty(t, cid2)
	assert.NotEqual(t, cid1, cid2)

	// Body Truncation Test
	largeBody := []byte("Hello World Large Payload")
	truncated := telemetry.TruncateBody(largeBody, 11)
	assert.Equal(t, "Hello World... [truncated 14 bytes]", truncated)

	// Slog Valuer Test
	info := &telemetry.TraceInfo{
		CorrelationID: cid1,
		RemoteAddr:    "127.0.0.1:443",
	}

	val := info.LogValue()
	assert.Equal(t, slog.KindGroup, val.Kind())
}

func TestExtractRedirectHistory(t *testing.T) {
	t.Parallel()

	req1, _ := http.NewRequestWithContext(t.Context(), "GET", "http://example.com/step1", nil)
	resp1 := &http.Response{StatusCode: 301, Request: req1}

	req2, _ := http.NewRequestWithContext(t.Context(), "GET", "https://example.com/step2", nil)
	req2.Response = resp1
	resp2 := &http.Response{StatusCode: 302, Request: req2}

	reqFinal, _ := http.NewRequestWithContext(t.Context(), "GET", "https://example.com/final", nil)
	reqFinal.Response = resp2
	respFinal := &http.Response{StatusCode: 200, Request: reqFinal}

	hops := telemetry.ExtractRedirectHistory(respFinal)
	require.Len(t, hops, 2)

	assert.Equal(t, "http://example.com/step1", hops[0].URL)
	assert.Equal(t, 301, hops[0].StatusCode)

	assert.Equal(t, "https://example.com/step2", hops[1].URL)
	assert.Equal(t, 302, hops[1].StatusCode)
}

func TestTraceInfo_OptionalHelpers(t *testing.T) {
	t.Parallel()

	info := &telemetry.TraceInfo{
		DNSLookup:    12 * time.Millisecond,
		TLSHandshake: 0, // Not performed (e.g. plain HTTP)
	}

	dnsOpt := info.DNSDuration()
	assert.True(t, dnsOpt.IsPresent())
	dnsVal, ok := dnsOpt.Value()
	assert.True(t, ok)
	assert.Equal(t, 12*time.Millisecond, dnsVal)

	tlsOpt := info.TLSDuration()
	assert.False(t, tlsOpt.IsPresent())

	ja4Opt := info.JA4Report()
	assert.False(t, ja4Opt.IsPresent())
}
