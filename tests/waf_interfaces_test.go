// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/resiliency/cache"
	"github.com/lemon4ksan/aoni/telemetry"
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

	client := aoni.NewClient(nil)
	if transport := client.Transport(); transport != nil {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client = client.With(
		option.WithBaseURL(server.URL),
		option.WithTLSClientHelloSpecProvider(provider),
		option.WithTLSFingerprint(aoni.BrowserChrome), // Needed to activate utls dialer
	)

	// We capture the report to verify
	var report ja4.Report

	client = client.With(option.WithJA4Callback(func(r ja4.Report) {
		report = r
	}))

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })

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

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithSocketController(controller),
	)

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })

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
	client := aoni.NewClient(server.Client()).With(
		option.WithBaseURL(server.URL),
		option.WithHTTP2Configurer(configurer),
	)

	resp, err := client.Request(t.Context(), http.MethodGet, "/")
	require.NoError(t, err)
	t.Cleanup(func() { aoni.CloseResponse(resp) })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Greater(t, atomic.LoadInt32(&configurer.called), int32(0))
}

func TestClient_E2E_WAFBypass_Pipeline(t *testing.T) {
	var wafHits atomic.Int32

	wafServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wafHits.Add(1)

		if !strings.Contains(r.Header.Get("User-Agent"), "Chrome") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><body>cf-challenge: blocked UA</body></html>"))

			return
		}

		cookie, err := r.Cookie("cf_clearance")
		if err != nil || cookie.Value != "YumYumCookie" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><body>cf-challenge: blocked cookies</body></html>"))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "evaded", "status": 200}`))
	}))
	wafServer.EnableHTTP2 = true
	wafServer.StartTLS()
	t.Cleanup(wafServer.Close)

	var proxyHits atomic.Int32

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)

		if r.Method == http.MethodConnect {
			destConn, err := net.Dial("tcp", r.Host)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}

			w.WriteHeader(http.StatusOK)

			hijacker, ok := w.(http.Hijacker)
			if !ok {
				return
			}

			clientConn, _, err := hijacker.Hijack()
			if err != nil {
				return
			}

			go func() {
				defer clientConn.Close()
				defer destConn.Close()

				done := make(chan struct{}, 2)
				go func() {
					_, _ = io.Copy(destConn, clientConn)

					done <- struct{}{}
				}()
				go func() {
					_, _ = io.Copy(clientConn, destConn)

					done <- struct{}{}
				}()

				<-done
			}()

			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	t.Cleanup(proxyServer.Close)

	jar := cookie.NewProxyIsolatedJar()
	u, err := url.Parse(wafServer.URL)
	require.NoError(t, err)

	jar.SetCookiesForProxy(proxyServer.URL, u, []*http.Cookie{
		{
			Name:     "cf_clearance",
			Value:    "YumYumCookie",
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
		},
	})

	cacheStore := cache.NewInMemoryStore(5 * time.Minute)
	harGen := telemetry.NewHARGenerator()

	client := aoni.NewClient(wafServer.Client(),
		option.WithBaseURL(wafServer.URL),
		option.WithBrowserProfile(aoni.BrowserChrome, profiles.Windows),
		option.WithInsecureSkipVerify(),
		option.WithCookieJar(jar),
		option.WithProxyString(proxyServer.URL),
		option.WithPipeline(aoni.PipelineConfig{
			Cache:      &aoni.CacheConfig{Store: cacheStore, DefaultTTL: 5 * time.Minute},
			HAR:        &aoni.HARConfig{Tracker: harGen},
			Validate:   true,
			Decompress: true,
		}),
		option.WithTCPDelay(1*time.Millisecond, 5*time.Millisecond),
	)

	info := &telemetry.TraceInfo{}
	result, err := request.GetTo[testPayload](t.Context(), client, "/", mod.WithTrace(info), mod.WithTraceJA4(info))
	require.NoError(t, err)

	assert.Equal(t, "evaded", result.Message)
	assert.Equal(t, int32(1), proxyHits.Load())
	assert.Equal(t, int32(1), wafHits.Load())

	assert.NotEmpty(t, info.JA4.JA4H)
	assert.NotEmpty(t, info.RemoteAddr)
	assert.Greater(t, info.Total, 0*time.Second)

	resultCached, err := request.GetTo[testPayload](t.Context(), client, "/")
	require.NoError(t, err)

	assert.Equal(t, "evaded", resultCached.Message)
	assert.Equal(t, int32(1), wafHits.Load())

	harBytes, err := harGen.Export()
	require.NoError(t, err)
	assert.Contains(t, string(harBytes), "evaded")
	assert.Contains(t, string(harBytes), "GET")
}
