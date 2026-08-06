// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
)

// HTTPDoer executes an HTTP request transaction.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPDoerFunc adapts a plain function matching the HTTP execution signature to the [HTTPDoer] interface.
type HTTPDoerFunc func(req *http.Request) (*http.Response, error)

// Do executes the underlying function against the provided HTTP request.
func (f HTTPDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// DefaultEngine standardizes the client to a uniform [HTTPDoer] interface.
//
// Supports [RequestDoer], [*http.Client] and [HTTPDoer].
// Falls back to a default http.Client with 15 second timeout and [DefaultRedirectPolicy].
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

// CloneHTTPClient produces a deep copy of an [*http.Client] and its transport wrappers.
func CloneHTTPClient(c *http.Client) *http.Client {
	cloned := *c
	baseTr := cloned.Transport

	var wrappedJar *cookie.ProxyIsolatedJar
	if cjTr, ok := baseTr.(*cookie.Transport); ok {
		wrappedJar = cjTr.CookieJar
		baseTr = cjTr.Next
	}

	var framedTr *h2.FramedTransport
	if ft, ok := baseTr.(*h2.FramedTransport); ok {
		framedTr = ft
		baseTr = ft.Transport
	}

	if tr, ok := baseTr.(*http.Transport); ok && tr != nil {
		baseTr = tr.Clone()
	}

	if framedTr != nil {
		if tr, ok := baseTr.(*http.Transport); ok {
			baseTr = framedTr.Clone(tr)
		}
	}

	if wrappedJar != nil {
		cloned.Transport = &cookie.Transport{
			Next:      baseTr,
			CookieJar: wrappedJar,
		}
	} else {
		cloned.Transport = baseTr
	}

	return &cloned
}

func (c *Client) reapplyH2Settings(tr *http.Transport) {
	if tr == nil {
		return
	}

	if c.fingerprint.H2Configurer != nil {
		t2, err := http2.ConfigureTransports(tr)
		if err == nil && t2 != nil {
			t2.TLSClientConfig = tr.TLSClientConfig
			_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
		}
	}

	if c.fingerprint.H2Settings == nil {
		return
	}

	framed := h2.NewFramedTransport(tr, *c.fingerprint.H2Settings)

	httpClient, ok := c.engine.(*http.Client)
	if !ok {
		return
	}

	if cjTrans, ok := httpClient.Transport.(*cookie.Transport); ok {
		cjTrans.Next = framed
	} else {
		httpClient.Transport = framed
	}
}

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
	applyCookieJar(c, httpClient, eng.CookieJar)
	applyTransportOverrides(c, eng)
}

func applyCookieJar(c *Client, httpClient *http.Client, jar http.CookieJar) {
	if jar == nil {
		return
	}

	httpClient.Jar = jar

	pJar, ok := jar.(*cookie.ProxyIsolatedJar)
	if !ok {
		return
	}

	c.defaults.HeadersCookieJar = jar

	baseTr := httpClient.Transport
	if baseTr == nil {
		baseTr = http.DefaultTransport
	}

	if cjTrans, ok := baseTr.(*cookie.Transport); ok {
		baseTr = cjTrans.Unwrap()
	}

	httpClient.Transport = &cookie.Transport{Next: baseTr, CookieJar: pJar}
}

func applyTransportOverrides(c *Client, eng EngineConfig) {
	tr := c.Transport()
	if tr == nil {
		return
	}

	if eng.InsecureSkipVerify {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}

		tr.TLSClientConfig.InsecureSkipVerify = true
	}

	if pool := eng.ConnectionPool; pool != nil {
		tr.MaxIdleConns = generic.Coalesce(pool.MaxIdleConns, tr.MaxIdleConns)
		tr.MaxIdleConnsPerHost = generic.Coalesce(pool.MaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
		tr.MaxConnsPerHost = generic.Coalesce(pool.MaxConnsPerHost, tr.MaxConnsPerHost)
		tr.IdleConnTimeout = generic.Coalesce(pool.IdleConnTimeout, tr.IdleConnTimeout)
		tr.ResponseHeaderTimeout = generic.Coalesce(pool.ResponseHeaderTimeout, tr.ResponseHeaderTimeout)
		tr.ReadBufferSize = generic.Coalesce(pool.ReadBufferSize, tr.ReadBufferSize)
		tr.WriteBufferSize = generic.Coalesce(pool.WriteBufferSize, tr.WriteBufferSize)
	}

	if h2Cfg := eng.HTTP2Config; h2Cfg != nil {
		if t2, err := http2.ConfigureTransports(tr); err == nil && t2 != nil {
			t2.ReadIdleTimeout = h2Cfg.ReadIdleTimeout
			t2.PingTimeout = h2Cfg.PingTimeout
			t2.AllowHTTP = h2Cfg.AllowHTTP
		}
	}
}
