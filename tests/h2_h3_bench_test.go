// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
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

	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
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

func BenchmarkH3_FastEngine(b *testing.B) {
	tlsCert, err := generateBenchTLSCert()
	if err != nil {
		b.Fatalf("failed to generate cert: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","engine":"fast_h3"}`))
	})

	h3Server := &http3.Server{
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		b.Fatalf("failed to listen UDP: %v", err)
	}
	defer udpConn.Close()

	go func() {
		_ = h3Server.Serve(udpConn)
	}()

	defer h3Server.Close()

	targetURL := "https://" + udpConn.LocalAddr().String()

	client := fast.NewClient(
		option.WithBaseURL(targetURL),
		option.WithInsecureSkipVerify(),
	)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		resp, err := client.Request(ctx, "GET", "/", mod.WithALPN(aoni.AlpnH3))
		if err != nil {
			b.Fatalf("fast h3 request failed: %v", err)
		}

		if resp.StatusCode() != http.StatusOK {
			_ = resp.Close()
			b.Fatalf("unexpected status: %d", resp.StatusCode())
		}

		_ = resp.Close()
	}
}

func BenchmarkH3_NetHTTP_QuicGo(b *testing.B) {
	tlsCert, err := generateBenchTLSCert()
	if err != nil {
		b.Fatalf("failed to generate cert: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","engine":"net_http_h3"}`))
	})

	h3Server := &http3.Server{
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		},
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		b.Fatalf("failed to listen UDP: %v", err)
	}
	defer udpConn.Close()

	go func() {
		_ = h3Server.Serve(udpConn)
	}()

	defer h3Server.Close()

	targetURL := "https://" + udpConn.LocalAddr().String()

	h3Transport := &http3.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	defer h3Transport.Close()

	stdClient := &http.Client{
		Transport: h3Transport,
	}
	client := aoni.NewClient(stdClient, option.WithBaseURL(targetURL))

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		resp, err := client.Request(ctx, "GET", "/", mod.WithALPN(aoni.AlpnH3))
		if err != nil {
			b.Fatalf("net/http h3 request failed: %v", err)
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			_ = resp.Body.Close()
			b.Fatalf("unexpected status: %d", resp.StatusCode)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}
