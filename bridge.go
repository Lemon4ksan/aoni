// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/url"
)

// modsCtxKey is the context key for carrying RequestModifiers through context.
type modsCtxKey struct{}

// NewStdClient returns a standard [*http.Client] whose transport routes all
// requests through the configured aoni [Client] pipeline.
//
// The returned client has Jar set to nil to avoid double cookie handling —
// the aoni [ProxyIsolatedCookieJar] manages cookies internally.
//
// Usage:
//
//	client := aoni.NewClient(nil).
//	    WithTLSFingerprint(aoni.BrowserChrome).
//	    WithDoHResolver()
//	stdClient := aoni.NewStdClient(client)
//
//	// Use with any third-party library
//	restyClient.SetHTTPClient(stdClient)
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: &aoniTransport{client: c},
		Jar:       nil,
	}
}

// WithContextModifier returns a new context carrying the given RequestModifiers.
// Third-party libraries that pass context through [http.Request] will carry
// these modifiers into the aoni pipeline automatically.
//
// Example with go-resty:
//
//	ctx := aoni.WithContextModifier(context.Background(),
//	    aoni.WithHeader("X-Api-Key", "secret"),
//	    aoni.TraceJA4(info),
//	)
//	resp, err := restyClient.R().SetContext(ctx).Get("/api/data")
func WithContextModifier(ctx context.Context, mods ...RequestModifier) context.Context {
	if len(mods) == 0 {
		return ctx
	}

	return context.WithValue(ctx, modsCtxKey{}, mods)
}

// ContextModifiers extracts the RequestModifiers previously stored via
// [WithContextModifier]. Returns nil if none are present.
func ContextModifiers(ctx context.Context) []RequestModifier {
	mods, _ := ctx.Value(modsCtxKey{}).([]RequestModifier)

	return mods
}

// aoniTransport implements [http.RoundTripper] by routing requests through
// a configured aoni [Client] pipeline.
type aoniTransport struct {
	client *Client
}

// RoundTrip extracts modifiers from the request context, applies them to the
// request, and delegates to the full aoni pipeline (SSRF guard, Happy Eyeballs,
// uTLS/JA4, middleware, proxy rotation, decompression, etc.).
func (t *aoniTransport) RoundTrip(origReq *http.Request) (*http.Response, error) {
	ctxMods := ContextModifiers(origReq.Context())

	syncModifier := func(req *http.Request) {
		req.Header = origReq.Header.Clone()
		req.Body = origReq.Body
		req.ContentLength = origReq.ContentLength
		req.TransferEncoding = origReq.TransferEncoding
		req.Close = origReq.Close
		req.Host = origReq.Host
		req.GetBody = origReq.GetBody
	}

	cloned := t.client.Clone()

	cloned.baseURL = &url.URL{
		Scheme: origReq.URL.Scheme,
		Host:   origReq.URL.Host,
	}

	allMods := make([]RequestModifier, 0, 1+len(ctxMods))
	allMods = append(allMods, syncModifier)
	allMods = append(allMods, ctxMods...)

	return cloned.Request(
		origReq.Context(),
		origReq.Method,
		origReq.URL.RequestURI(),
		allMods...,
	)
}

// Ensure aoniTransport implements http.RoundTripper.
var _ http.RoundTripper = (*aoniTransport)(nil)
