// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/netutil/digest"
	"github.com/lemon4ksan/aoni/pipeline"
)

// DefaultEngine normalizes arbitrary execution targets (RequestDoer, *http.Client, HTTPDoer, or nil)
// into a standardized, production-ready [HTTPDoer] instance.
//
// Architectural Scope:
// This is a low-level pipeline utility primarily used internally by [NewClient] and
// custom engine adapters. Application code typically should use [NewClient] or [New] instead.
//
// Normalization resolution sequence:
//  1. Recursive Unwrapping: Traverses targets implementing [Unwrap], Rest(), or Requester().
//  2. Type-Switch Priority:
//     - [RequestDoer]: Wrapped via [NewRequestDoerAdapter].
//     - [*http.Client]: Deep-cloned via [CloneHTTPClient] to isolate socket pools.
//     - [HTTPDoer]: Accepted directly (e.g. mocks or test closures).
//  3. Default Fallback: Constructs a fresh [*http.Client] with 15s timeout, 10-hop redirect bounds,
//     and default production transport.
func DefaultEngine(doer any) HTTPDoer {
	if doer == nil {
		return &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: DefaultRedirectPolicy(10),
			Transport:     newDefaultTransport(),
		}
	}

	if unwrapper, ok := doer.(interface{ Unwrap() any }); ok {
		if inner := unwrapper.Unwrap(); inner != nil && inner != doer {
			return DefaultEngine(inner)
		}
	}

	if rd, ok := doer.(interface{ Rest() any }); ok {
		if inner := rd.Rest(); inner != nil && inner != doer {
			return DefaultEngine(inner)
		}
	}

	if rd, ok := doer.(interface{ Requester() any }); ok {
		if inner := rd.Requester(); inner != nil && inner != doer {
			return DefaultEngine(inner)
		}
	}

	if reqLike, ok := doer.(requesterLike); ok && reqLike != nil {
		return requesterHTTPDoer{r: reqLike}
	}

	switch doer := doer.(type) {
	case RequestDoer:
		if doer != nil {
			return NewRequestDoerAdapter(doer)
		}
	case *http.Client:
		if doer != nil {
			return CloneHTTPClient(doer)
		}
	case HTTPDoer:
		if doer != nil {
			return doer
		}
	}

	return &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: DefaultRedirectPolicy(10),
		Transport:     newDefaultTransport(),
	}
}

func newDefaultTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		// RFC 8996 & RFC 7525: Minimum version MUST be TLS 1.2.
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (c *Client) reapplyH2Settings(tr *http.Transport) {}

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

	if httpClient.Transport == nil {
		httpClient.Transport = newDefaultTransport()
	}

	if eng.Timeout > 0 {
		httpClient.Timeout = eng.Timeout
	}

	applyRedirectPolicy(httpClient, eng)
	applyCookieJar(httpClient, eng.CookieJar)
	applyDigestAuth(httpClient, eng.DigestAuth)
	applyTransportOverrides(c, eng)
}

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

	if cjTrans, ok := baseTr.(*cookie.Transport); ok {
		baseTr = cjTrans.Unwrap()
	}

	httpClient.Transport = &cookie.Transport{Next: baseTr, CookieJar: pJar}
}

func applyDigestAuth(httpClient *http.Client, digestCfg *DigestAuthConfig) {
	if digestCfg == nil || digestCfg.Username == "" {
		return
	}

	baseTr := httpClient.Transport
	if baseTr == nil {
		baseTr = http.DefaultTransport
	}

	if dt, ok := baseTr.(*digest.Transport); ok {
		baseTr = dt.Unwrap()
	}

	httpClient.Transport = &digest.Transport{
		Username:  digestCfg.Username,
		Password:  digestCfg.Password,
		Transport: baseTr,
	}
}

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
