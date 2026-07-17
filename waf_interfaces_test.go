// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	utls "github.com/refraction-networking/utls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/ja4"
)

type mockSpecProvider struct {
	spec   *utls.ClientHelloSpec
	called int32
}

func (m *mockSpecProvider) ClientHelloSpec() (*utls.ClientHelloSpec, error) {
	atomic.AddInt32(&m.called, 1)
	return m.spec, nil
}

func TestClient_ClientHelloSpecProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Fetch a base spec (don't override ciphers so handshake succeeds with standard server)
	baseSpec, err := utls.UTLSIdToSpec(utls.HelloChrome_102)
	require.NoError(t, err)

	provider := &mockSpecProvider{spec: &baseSpec}

	client := NewClient(nil)
	if transport := client.Transport(); transport != nil {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client = client.With(
		WithClientBaseURL(server.URL),
		WithClientTLSClientHelloSpecProvider(provider),
		WithClientTLSFingerprint(BrowserChrome), // Needed to activate utls dialer
	)

	// We capture the report to verify
	var report ja4.Report

	client = client.With(WithClientJA4Callback(func(r ja4.Report) {
		report = r
	}))

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { closeResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, report.JA4)
	assert.Greater(t, atomic.LoadInt32(&provider.called), int32(0))
}

type mockSocketController struct {
	called int32
}

func (m *mockSocketController) Control(fd uintptr, network, address string) error {
	atomic.AddInt32(&m.called, 1)
	return nil
}

func TestClient_SocketController(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	controller := &mockSocketController{}

	client := NewClient(nil,
		WithClientBaseURL(server.URL),
		WithClientSocketController(controller),
	)

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { closeResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Greater(t, atomic.LoadInt32(&controller.called), int32(0))
}

type mockHTTP2Configurer struct {
	called int32
}

func (m *mockHTTP2Configurer) ConfigureHTTP2(t *http2.Transport) error {
	atomic.AddInt32(&m.called, 1)

	t.MaxEncoderHeaderTableSize = 4096

	return nil
}

func TestClient_HTTP2Configurer(t *testing.T) {
	t.Parallel()

	// HTTP/2 requires TLS to negotiate ALPN "h2".
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	configurer := &mockHTTP2Configurer{}

	// Use the httptest server's own client - it already trusts the self-signed cert.
	// Build the aoni Client on top of it so TLS config is correct from the start.
	client := NewClient(server.Client()).With(
		WithClientBaseURL(server.URL),
		WithClientHTTP2Configurer(configurer),
	)

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { closeResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Greater(t, atomic.LoadInt32(&configurer.called), int32(0))
}
