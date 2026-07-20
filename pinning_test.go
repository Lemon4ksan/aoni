// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCertificatePinning(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	// Extract port from the test server URL
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)

	// Extract the leaf certificate of the test server
	require.NotEmpty(t, server.TLS.Certificates)
	leafCert, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)

	// Compute SPKI fingerprints
	spkiHash := sha256.Sum256(leafCert.RawSubjectPublicKeyInfo)
	correctPinBase64 := base64.StdEncoding.EncodeToString(spkiHash[:])
	correctPinHex := hex.EncodeToString(spkiHash[:])
	correctPinPrefixed := "sha256/" + correctPinBase64

	incorrectPin := "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

	t.Run("Standard Client - Correct Pin Base64", func(t *testing.T) {
		client := NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			WithCertificatePin("127.0.0.1", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Standard Client - Correct Pin Hex", func(t *testing.T) {
		client := NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			WithCertificatePin("127.0.0.1", correctPinHex),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Standard Client - Correct Pin Prefixed", func(t *testing.T) {
		client := NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			WithCertificatePin("127.0.0.1", correctPinPrefixed),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Standard Client - Incorrect Pin", func(t *testing.T) {
		client := NewClient(server.Client())
		_, err := client.Request(t.Context(), http.MethodGet, server.URL,
			WithCertificatePin("127.0.0.1", incorrectPin),
		)
		require.Error(t, err)
		assert.True(
			t,
			errors.Is(err, ErrCertificatePinning) ||
				strings.Contains(err.Error(), "certificate pinning validation failed"),
		)
	})

	t.Run("UTLS Client - Correct Pin", func(t *testing.T) {
		client := NewClient(nil, withTLSFingerprint(BrowserChrome))
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			WithCertificatePin("127.0.0.1", correctPinBase64),
			WithInsecureSkipVerify(),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("UTLS Client - Incorrect Pin", func(t *testing.T) {
		client := NewClient(nil, withTLSFingerprint(BrowserChrome))
		_, err := client.Request(t.Context(), http.MethodGet, server.URL,
			WithCertificatePin("127.0.0.1", incorrectPin),
			WithInsecureSkipVerify(),
		)
		require.Error(t, err)
		assert.True(
			t,
			errors.Is(err, ErrCertificatePinning) ||
				strings.Contains(err.Error(), "certificate pinning validation failed"),
		)
	})

	t.Run("Wildcard Domain Match - Correct Pin", func(t *testing.T) {
		client := NewClient(server.Client())
		// Map api.example.com to our local test server port
		targetURL := "https://api.example.com/test"
		resp, err := client.Request(t.Context(), http.MethodGet, targetURL,
			WithHostRewrite(map[string]string{"api.example.com": "127.0.0.1:" + port}),
			WithCertificatePin("*.example.com", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Suffix Domain Match - Correct Pin", func(t *testing.T) {
		client := NewClient(server.Client())
		// Map api.example.com to our local test server port
		targetURL := "https://api.example.com/test"
		resp, err := client.Request(t.Context(), http.MethodGet, targetURL,
			WithHostRewrite(map[string]string{"api.example.com": "127.0.0.1:" + port}),
			WithCertificatePin(".example.com", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Multiple Pins - One Correct", func(t *testing.T) {
		client := NewClient(server.Client())
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL,
			WithCertificatePin("127.0.0.1", incorrectPin),
			WithCertificatePin("127.0.0.1", correctPinBase64),
		)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Client-Level Correct Pin", func(t *testing.T) {
		client := NewClient(server.Client(), withCertificatePin("127.0.0.1", correctPinBase64))
		resp, err := client.Request(t.Context(), http.MethodGet, server.URL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Client-Level Incorrect Pin", func(t *testing.T) {
		client := NewClient(server.Client(), withCertificatePin("127.0.0.1", incorrectPin))
		_, err := client.Request(t.Context(), http.MethodGet, server.URL)
		require.Error(t, err)
		assert.True(
			t,
			errors.Is(err, ErrCertificatePinning) ||
				strings.Contains(err.Error(), "certificate pinning validation failed"),
		)
	})
}
