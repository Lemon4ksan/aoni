// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// HTTPDoer specifies the minimal execution contract for processing standard *http.Request transactions.
// It matches the exact signature of standard library [*http.Client.Do].
//
// Thread Safety:
// Implementations MUST be fully thread-safe and safe for concurrent invocation across goroutines.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPDoerFunc adapts a plain execution closure to the [HTTPDoer] interface.
type HTTPDoerFunc func(req *http.Request) (*http.Response, error)

// Do executes the underlying closure against the provided HTTP request.
func (f HTTPDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// DefaultEngine normalizes arbitrary execution targets (RequestDoer, *http.Client, HTTPDoer, or nil)
// into a standardized, production-ready [HTTPDoer] instance.
//
// Normalization Semantics:
//   - nil: Instantiates a default *http.Client with 15s timeout and 10-redirect limit.
//   - RequestDoer: Adapts via NewRequestDoerAdapter.
//   - *http.Client: Deep-clones the client and its transport layers via CloneHTTPClient.
//   - HTTPDoer: Used directly as-is.
//
// Calling DefaultEngine on doer passed to a [NewClient] is redundant.
func DefaultEngine(doer any) HTTPDoer {
	if doer != nil {
		if rd, ok := doer.(RequestDoer); ok {
			return NewRequestDoerAdapter(rd)
		}

		if httpClient, ok := doer.(*http.Client); ok {
			return CloneHTTPClient(httpClient)
		}

		if hd, ok := doer.(HTTPDoer); ok {
			return hd
		}
	}

	return &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: DefaultRedirectPolicy(10),
	}
}

// TransportUnwrapper is implemented by RoundTripper decorators that wrap an inner RoundTripper.
type TransportUnwrapper interface {
	Unwrap() http.RoundTripper
}

// TransportCloner is implemented by RoundTripper decorators capable of creating a deep-copy
// of themselves around a newly cloned inner RoundTripper.
type TransportCloner interface {
	CloneTransport(next http.RoundTripper) http.RoundTripper
}

// CloneHTTPClient produces a deep, memory-isolated copy of an [*http.Client] and its nested transport layers.
//
// Copying an *http.Client by value (e.g. *c) performs a shallow copy of its internal pointers.
// Since http.Client.Transport is a pointer to http.Transport, two shallow-copied clients share the exact same
// transport instance. Mutating TLSClientConfig, MaxIdleConns, or headers on one client would cause DATA RACES
// and corrupt the other client's connection state.
//
// It uses TransportUnwrapper and TransportCloner interfaces to recursively peel off and deep-clone
// arbitrarily nested transport decorator chains down to the base *http.Transport.
func CloneHTTPClient(c *http.Client) *http.Client {
	if c == nil {
		return nil
	}

	cloned := *c
	if cloned.Transport == nil {
		return &cloned
	}

	cloned.Transport = cloneRoundTripper(cloned.Transport)

	return &cloned
}

func cloneRoundTripper(tr http.RoundTripper) http.RoundTripper {
	if tr == nil {
		return nil
	}

	if cloner, ok := tr.(TransportCloner); ok {
		if unwrapper, ok := tr.(TransportUnwrapper); ok {
			nextCloned := cloneRoundTripper(unwrapper.Unwrap())
			return cloner.CloneTransport(nextCloned)
		}
	}

	if unwrapper, ok := tr.(TransportUnwrapper); ok {
		return cloneRoundTripper(unwrapper.Unwrap())
	}

	if baseTr, ok := tr.(*http.Transport); ok && baseTr != nil {
		return baseTr.Clone()
	}

	return tr
}

// reapplyH2Settings injects or updates HTTP/2 SETTINGS frame parameters and H2 transport configurers.
func (c *Client) reapplyH2Settings(tr *http.Transport) {
	if tr == nil {
		return
	}

	if c.fingerprint.H2Configurer != nil || c.fingerprint.H2Settings != nil {
		settings := h2.Settings{}
		if c.fingerprint.H2Settings != nil {
			settings = *c.fingerprint.H2Settings
		}

		framed := h2.NewFramedTransport(tr, settings)
		if c.fingerprint.H2Configurer != nil && framed.H2Transport() != nil {
			_ = c.fingerprint.H2Configurer.ConfigureHTTP2(framed.H2Transport())
		}

		httpClient, ok := c.engine.(*http.Client)
		if ok {
			if cjTrans, ok := httpClient.Transport.(*cookie.Transport); ok {
				cjTrans.Next = framed
			} else {
				httpClient.Transport = framed
			}
		}
	}
}

// applyEngineConfig applies timeouts, redirect policies, cookie jars, and transport pool bounds
// to the client's execution engine.
func applyEngineConfig(c *Client, eng EngineConfig) {
	if eng.CustomEngine != nil {
		if httpClient, ok := eng.CustomEngine.(*http.Client); ok {
			c.engine = CloneHTTPClient(httpClient)
		} else {
			c.engine = eng.CustomEngine
			return
		}
	}

	httpClient, ok := c.engine.(*http.Client)
	if !ok {
		return
	}

	if eng.Timeout > 0 {
		httpClient.Timeout = eng.Timeout
	}

	applyRedirectPolicy(httpClient, eng)
	applyCookieJar(httpClient, eng.CookieJar)
	applyTransportOverrides(c, eng)
}

// applyCookieJar wraps the engine's transport layer in a Proxy-Isolated Cookie Transport
// if jar implements cookie.ProxyIsolatedJar.
func applyCookieJar(httpClient *http.Client, jar http.CookieJar) {
	if jar == nil {
		return
	}

	httpClient.Jar = jar

	pJar, ok := jar.(*cookie.ProxyIsolatedJar)
	if !ok {
		return
	}

	baseTr := httpClient.Transport
	if baseTr == nil {
		baseTr = http.DefaultTransport
	}

	// Unwrap existing cookie transport layer if present to avoid double-wrapping
	if cjTrans, ok := baseTr.(*cookie.Transport); ok {
		baseTr = cjTrans.Unwrap()
	}

	httpClient.Transport = &cookie.Transport{Next: baseTr, CookieJar: pJar}
}

// applyTransportOverrides applies TLS verification flags, connection pool limits,
// and HTTP/2 protocol parameters directly to the underlying *http.Transport.
func applyTransportOverrides(c *Client, eng EngineConfig) {
	tr := c.Transport()
	if tr == nil {
		return
	}

	var poolDTO *pipeline.ConnectionPoolConfigDTO
	if pool := eng.ConnectionPool; pool != nil {
		poolDTO = &pipeline.ConnectionPoolConfigDTO{
			MaxIdleConns:          pool.MaxIdleConns,
			MaxIdleConnsPerHost:   pool.MaxIdleConnsPerHost,
			MaxConnsPerHost:       pool.MaxConnsPerHost,
			IdleConnTimeout:       pool.IdleConnTimeout,
			ResponseHeaderTimeout: pool.ResponseHeaderTimeout,
			ReadBufferSize:        pool.ReadBufferSize,
			WriteBufferSize:       pool.WriteBufferSize,
		}
	}

	var h2DTO *pipeline.HTTP2ConfigDTO
	if h2Cfg := eng.HTTP2Config; h2Cfg != nil {
		h2DTO = &pipeline.HTTP2ConfigDTO{
			ReadIdleTimeout: h2Cfg.ReadIdleTimeout,
			PingTimeout:     h2Cfg.PingTimeout,
			AllowHTTP:       h2Cfg.AllowHTTP,
		}
	}

	pipeline.ApplyTransportOverrides(tr, eng.InsecureSkipVerify, poolDTO, h2DTO)
}
