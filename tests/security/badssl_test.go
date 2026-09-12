// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package security

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
)

func TestBadSSLDocker(t *testing.T) {
	var client *aoni.Client

	if os.Getenv("BADSSL_DOCKER") != "" {
		caCert, err := os.ReadFile("d:/CodingProjects/badssl.com/certs/sets/test/gen/crt/ca-root.crt")
		if err == nil {
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			tlsConfig := &tls.Config{RootCAs: caCertPool}

			transport := &http.Transport{
				TLSClientConfig: tlsConfig,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return net.DialTimeout("tcp", "127.0.0.1:443", 5*time.Second)
				},
			}

			client = aoni.NewClient(&http.Client{Transport: transport})
		}
	} else {
		client = aoni.NewClient(nil)
	}

	domainSuffix := ".badssl.test"
	if os.Getenv("BADSSL_DOCKER") == "" {
		domainSuffix = ".badssl.com"
	}

	tests := []struct {
		subdomain   string
		expectError bool
		errString   string
	}{
		{"", false, ""},
		{"expired", true, "certificate has expired"},
		{"wrong.host", true, "certificate is valid for"},
		{"self-signed", true, "signed by unknown authority"},
		{"untrusted-root", true, "signed by unknown authority"},
		{"rc4", true, "remote error: tls: handshake failure"},
		{"dh480", true, "handshake failure"},
	}

	for _, tc := range tests {
		url := "https://"
		if tc.subdomain != "" {
			url += tc.subdomain + domainSuffix
		} else {
			if domainSuffix == ".badssl.com" {
				url += "badssl.com"
			} else {
				url += "badssl.test"
			}
		}

		t.Run(url, func(t *testing.T) {
			resp, err := client.Get(context.Background(), url)
			if tc.expectError {
				if err == nil {
					t.Fatalf("Expected error for %s, got status %d", url, resp.StatusCode)
				}
				if tc.errString != "" && !strings.Contains(err.Error(), tc.errString) {
					t.Errorf("Expected error containing %q, got: %v", tc.errString, err)
				}
			} else {
				if err != nil {
					t.Fatalf("Expected success for %s, got err: %v", url, err)
				}
				if resp.StatusCode != 200 {
					t.Errorf("Expected status 200, got %d", resp.StatusCode)
				}
			}
		})
	}
}
