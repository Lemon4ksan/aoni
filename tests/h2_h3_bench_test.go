// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
	"github.com/lemon4ksan/aoni/internal/fast/h3engine"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func generateBenchTLSCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Benchmark Org"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost", "127.0.0.1"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return tls.X509KeyPair(certPEM, keyPEM)
}

func BenchmarkH2_FastEngine(b *testing.B) {
	tlsCert, err := generateBenchTLSCert()
	if err != nil {
		b.Fatalf("failed to generate cert: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","engine":"fast_h2"}`))
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"h2"},
	}
	_ = http2.ConfigureServer(srv.Config, &http2.Server{})

	srv.StartTLS()
	defer srv.Close()

	client := fast.NewClient(
		option.WithBaseURL(srv.URL),
		option.WithInsecureSkipVerify(),
	)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		resp, err := client.Request(ctx, "GET", "/", mod.WithALPN(aoni.AlpnH2))
		if err != nil {
			b.Fatalf("fast h2 request failed: %v", err)
		}

		if resp.StatusCode() != http.StatusOK {
			_ = resp.Close()
			b.Fatalf("unexpected status: %d", resp.StatusCode())
		}

		_ = resp.Close()
	}
}

func BenchmarkH2_NetHTTP(b *testing.B) {
	tlsCert, err := generateBenchTLSCert()
	if err != nil {
		b.Fatalf("failed to generate cert: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","engine":"net_http_h2"}`))
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"h2"},
	}
	_ = http2.ConfigureServer(srv.Config, &http2.Server{})

	srv.StartTLS()
	defer srv.Close()

	stdClient := &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	client := aoni.NewClient(stdClient, option.WithBaseURL(srv.URL))

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		resp, err := client.Request(ctx, "GET", "/", mod.WithALPN(aoni.AlpnH2))
		if err != nil {
			b.Fatalf("net/http h2 request failed: %v", err)
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			_ = resp.Body.Close()
			b.Fatalf("unexpected status: %d", resp.StatusCode)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func BenchmarkH3_QPACK_Block_ZeroAlloc(b *testing.B) {
	codec := h3engine.NewQPACKCodec()
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI("https://api.example.com/v2/users")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-aoni-version", "2.0.0")
	req.Header.Set("user-agent", "aoni/2.0")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		p := codec.AcquireEncoder()
		_, err := codec.EncodeRequestHeadersPooled(p, req, nil)
		if err != nil {
			b.Fatalf("failed to encode: %v", err)
		}
		codec.ReleaseEncoder(p)
	}
}

func BenchmarkH3_QPACK_EncodeDecode(b *testing.B) {
	codec := h3engine.NewQPACKCodec()
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI("https://api.example.com/v2/users")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-aoni-version", "2.0.0")
	req.Header.Set("user-agent", "aoni/2.0")

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		buf.Reset()
		if err := codec.EncodeRequestHeaders(&buf, req, nil); err != nil {
			b.Fatalf("failed to encode: %v", err)
		}
	}
}

func BenchmarkH2_HPACK_EncodeDecode(b *testing.B) {
	hpEnc := h2engine.AcquireHPACK()
	defer h2engine.ReleaseHPACK(hpEnc)

	hFrame := h2engine.AcquireFrame(h2engine.FrameHeaders).(*h2engine.Headers)
	defer h2engine.ReleaseFrame(hFrame)

	hf := h2engine.AcquireHeaderField()
	defer h2engine.ReleaseHeaderField(hf)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		hFrame.Reset()
		hf.Set(":method", "POST")
		hFrame.AppendHeaderField(hpEnc, hf, true)
		hf.Set(":path", "/v2/users")
		hFrame.AppendHeaderField(hpEnc, hf, true)
		hf.Set(":authority", "api.example.com")
		hFrame.AppendHeaderField(hpEnc, hf, true)
		hf.Set(":scheme", "https")
		hFrame.AppendHeaderField(hpEnc, hf, true)
		hf.Set("content-type", "application/json")
		hFrame.AppendHeaderField(hpEnc, hf, true)
		hf.Set("x-aoni-version", "2.0.0")
		hFrame.AppendHeaderField(hpEnc, hf, true)
		hf.Set("user-agent", "aoni/2.0")
		hFrame.AppendHeaderField(hpEnc, hf, true)
	}
}

func BenchmarkH3_FrameRoundtrip(b *testing.B) {
	codec := h3engine.NewQPACKCodec()
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI("https://api.example.com/v2/users")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-aoni-version", "2.0.0")
	req.Header.Set("user-agent", "aoni/2.0")

	var respHeader fasthttp.ResponseHeader

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		p := codec.AcquireEncoder()
		hdrBlock, err := codec.EncodeRequestHeadersPooled(p, req, nil)
		if err != nil {
			b.Fatalf("failed to encode: %v", err)
		}

		respHeader.Reset()
		_, _ = codec.DecodeResponseHeaders(hdrBlock, &respHeader)
		codec.ReleaseEncoder(p)
	}
}
