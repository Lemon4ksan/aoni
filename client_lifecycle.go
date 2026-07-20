// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni/cookie"
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

// CloneHTTPClient returns a deep cloned http client.
func CloneHTTPClient(c *http.Client) *http.Client {
	cloned := *c
	baseTr := cloned.Transport

	var wrappedJar *cookie.ProxyIsolatedJar

	if cjTr, ok := baseTr.(*cookie.Transport); ok {
		wrappedJar = cjTr.CookieJar
		baseTr = cjTr.Next
	}

	if tr, ok := baseTr.(*http.Transport); ok && tr != nil {
		baseTr = tr.Clone()
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

// With returns a clone of c with the specified functional options applied.
func (c *Client) With(opts ...ClientOption) *Client {
	cloned := c.Clone()
	generic.ApplyOptions(cloned, opts...)
	cloned.applyDialers(c.Transport())

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

	cloned.applyDialers(c.Transport())

	return cloned
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
