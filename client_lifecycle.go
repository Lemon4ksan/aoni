// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/lemon4ksan/miyako/generic"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/h2"
	"github.com/lemon4ksan/aoni/internal/io"
)

// Unwrapper allows nested decorators to be peeled away to reach the
// underlying [Requester]. [Client] does not implement this interface;
// wrapper types returned by [NewStdClient] or [Chain] do.
type Unwrapper interface {
	Unwrap() Requester
}

// UnwrapClient strips all [Unwrapper] layers from r and returns the
// innermost [Client]. Returns nil if r is not a *Client and no
// Unwrapper chain leads to one.
func UnwrapClient(r Requester) (c *Client) {
	for {
		if client, ok := r.(*Client); ok {
			return client
		}

		u, ok := r.(Unwrapper)
		if !ok {
			break
		}

		r = u.Unwrap()
	}

	return nil
}

// GetOrInitRequestConfig retrieves or initializes the [RequestConfig] associated with the request context.
func GetOrInitRequestConfig(req *http.Request) *RequestConfig {
	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = &RequestConfig{
			Metadata: make(map[string]any),
		}
		ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
		*req = *req.WithContext(ctx)
	}

	return cfg
}

// CloneHTTPClient returns a deep cloned http client.
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

// CloseResponse closes the response body and cancels the request timeout if applicable.
func CloseResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_ = resp.Body.Close()

	if rb, ok := io.UnwrapBody(resp.Body).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}

	if resp.Request != nil {
		cfg := GetRequestConfig(resp.Request.Context())
		if cfg != nil && cfg.RequestTimeoutCancel != nil {
			cfg.RequestTimeoutCancel()
		}
	}
}

// With returns a clone of c with the specified functional options applied.
//
// It works in three phases:
//  1. Assemble the current client state into a [Config] (deep-copied).
//  2. Apply every [ClientOption] to that Config — options only touch data.
//  3. Build a new *Client from the updated Config and apply transport-level
//     side-effects (dialers, TLS, H2 framing, engine settings).
func (c *Client) With(opts ...ClientOption) *Client {
	// Phase 1 – capture current state into a Config snapshot.
	cfg := Config{
		Network:     c.network.Clone(),
		Fingerprint: c.fingerprint.Clone(),
		Defaults:    c.defaults.Clone(),
		// Engine.* fields start at zero; they represent "no override" by default.
	}

	// Phase 2 – let every option mutate the Config snapshot.
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	// Phase 3 – build the new cloned client from the updated Config.
	cloned := &Client{
		network:     cfg.Network,
		fingerprint: cfg.Fingerprint,
		defaults:    cfg.Defaults,
	}

	// Clone the underlying engine independently from the config fields.
	cloned.engine = c.engine
	if httpClient, ok := cloned.engine.(*http.Client); ok {
		cloned.engine = CloneHTTPClient(httpClient)
	}

	// Apply engine-level overrides expressed through cfg.Engine.
	applyEngineConfig(cloned, cfg.Engine)

	// Re-wire transport dialers so that any Network/Fingerprint changes
	// (SSRFGuard, HappyEyeballs, proxy, SourceRotator, DNSResolver, etc.)
	// are reflected on the cloned transport.
	cloned.applyDialers(cloned.Transport())

	// Re-apply H2 framing / configurer if fingerprint settings changed.
	cloned.reapplyH2Settings(cloned.Transport())

	return cloned
}

// Clone returns a deep copy of c. The cloned client shares nothing
// mutable with the original - transport, cookie jar, and config
// structs are all independently copied.
func (c *Client) Clone() *Client {
	cloned := &Client{
		network:     c.network.Clone(),
		fingerprint: c.fingerprint.Clone(),
		defaults:    c.defaults.Clone(),
	}

	cloned.engine = c.engine
	if httpClient, ok := cloned.engine.(*http.Client); ok {
		cloned.engine = CloneHTTPClient(httpClient)
	}

	cloned.applyDialers(cloned.Transport())
	cloned.reapplyH2Settings(cloned.Transport())

	return cloned
}

// applyEngineConfig applies the engine-level overrides stored in [EngineConfig]
// to an already-constructed *Client. It is called by [Client.With] and [NewClient]
// after the client's data fields (network/fingerprint/defaults) have been set.
func applyEngineConfig(c *Client, eng EngineConfig) {
	// CustomEngine takes full precedence over everything else.
	if eng.CustomEngine != nil {
		c.engine = eng.CustomEngine
		return
	}

	httpClient, ok := c.engine.(*http.Client)
	if !ok {
		return
	}

	if eng.Timeout > 0 {
		httpClient.Timeout = eng.Timeout
	}

	if eng.RedirectLimit != redirectLimitUnset {
		switch {
		case eng.RedirectLimit == 0:
			httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		case eng.RedirectLimit > 0:
			httpClient.CheckRedirect = DefaultRedirectPolicy(eng.RedirectLimit)
		default:
			// negative (other than redirectLimitUnset sentinel) means unlimited
			httpClient.CheckRedirect = DefaultRedirectPolicy(10)
		}
	}

	if eng.CookieJar != nil {
		httpClient.Jar = eng.CookieJar

		if pJar, ok := eng.CookieJar.(*cookie.ProxyIsolatedJar); ok {
			c.defaults.HeadersCookieJar = eng.CookieJar

			baseTr := httpClient.Transport
			if baseTr == nil {
				baseTr = http.DefaultTransport
			}

			if cjTrans, ok := baseTr.(*cookie.Transport); ok {
				baseTr = cjTrans.Unwrap()
			}

			httpClient.Transport = &cookie.Transport{Next: baseTr, CookieJar: pJar}
		}
	}

	if eng.InsecureSkipVerify {
		if transport := c.Transport(); transport != nil {
			if transport.TLSClientConfig == nil {
				transport.TLSClientConfig = &tls.Config{} //nolint:gosec
			}

			transport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec
		}
	}

	if eng.ConnectionPool != nil {
		pool := eng.ConnectionPool
		if transport := c.Transport(); transport != nil {
			transport.MaxIdleConns = generic.Coalesce(pool.MaxIdleConns, transport.MaxIdleConns)
			transport.MaxIdleConnsPerHost = generic.Coalesce(pool.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
			transport.MaxConnsPerHost = generic.Coalesce(pool.MaxConnsPerHost, transport.MaxConnsPerHost)
			transport.IdleConnTimeout = generic.Coalesce(pool.IdleConnTimeout, transport.IdleConnTimeout)
			transport.ResponseHeaderTimeout = generic.Coalesce(
				pool.ResponseHeaderTimeout,
				transport.ResponseHeaderTimeout,
			)
		}
	}

	if eng.HTTP2Config != nil {
		if transport := c.Transport(); transport != nil {
			t2, err := http2.ConfigureTransports(transport)
			if err == nil && t2 != nil {
				t2.ReadIdleTimeout = eng.HTTP2Config.ReadIdleTimeout
				t2.PingTimeout = eng.HTTP2Config.PingTimeout
				t2.AllowHTTP = eng.HTTP2Config.AllowHTTP
			}
		}
	}
}

// InitRequestConfig initializes the request configuration for the given request.
func (c *Client) InitRequestConfig(req *http.Request) *http.Request {
	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = &RequestConfig{}
		ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
		req = req.WithContext(ctx)
	}

	cfg.ApplyDefaults(c)

	return req
}

// CloseIdleConnections closes any idle keep-alive connections maintained by the client.
// This only works if the underlying [HTTPDoer] is an [http.Client].
func (c *Client) CloseIdleConnections() {
	if httpClient, ok := c.engine.(*http.Client); ok {
		httpClient.CloseIdleConnections()
	}
}
