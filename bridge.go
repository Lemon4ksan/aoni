// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
)

// modsCtxKey is the context key for carrying RequestModifiers through context.
type modsCtxKey struct{}

// NewStdClient returns a standard [http.Client] whose transport routes all
// requests through the configured aoni [Client] pipeline.
//
// The returned client has Jar set to nil to avoid double cookie handling.
// The aoni [ProxyIsolatedCookieJar] manages cookies internally.
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
		Transport: NewTransport(c),
		Jar:       nil,
	}
}

// NewTransport returns a new [http.RoundTripper] (specifically [*Transport])
// configured to route all requests through the provided aoni [Client].
// This allows developers to integrate aoni's advanced transport features into
// existing [http.Client] instances simply by swapping the Transport field.
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
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

// AppendContextModifier appends new modifiers to an existing context carrying modifiers,
// or creates a new one if none are present.
func AppendContextModifier(ctx context.Context, mods ...RequestModifier) context.Context {
	if len(mods) == 0 {
		return ctx
	}

	existing := ContextModifiers(ctx)
	combined := make([]RequestModifier, 0, len(existing)+len(mods))
	combined = append(combined, existing...)
	combined = append(combined, mods...)

	return context.WithValue(ctx, modsCtxKey{}, combined)
}

// ContextModifiers extracts the RequestModifiers previously stored via
// [WithContextModifier]. Returns nil if none are present.
func ContextModifiers(ctx context.Context) []RequestModifier {
	mods, _ := ctx.Value(modsCtxKey{}).([]RequestModifier)

	return mods
}

// Transport implements [http.RoundTripper] by routing requests through
// a configured aoni [Client] pipeline.
type Transport struct {
	client *Client
}

// RoundTrip extracts modifiers from the request context, applies them to the
// request, and delegates to the full aoni pipeline (SSRF guard, Happy Eyeballs,
// uTLS/JA4, middleware, proxy rotation, decompression, etc.).
//
// In accordance with standard [http.RoundTripper] requirements, it returns errors
// wrapped as [*url.Error].
func (t *Transport) RoundTrip(origReq *http.Request) (*http.Response, error) {
	if origReq.URL == nil {
		return nil, &url.Error{
			Op:  origReq.Method,
			URL: "",
			Err: errors.New("aoni bridge: request URL is nil"),
		}
	}

	ctxMods := ContextModifiers(origReq.Context())

	// syncModifier copies request metadata from the original request.
	// Headers are MERGED: origReq headers are added on top of aoni's
	// defaults, so both global config and per-request headers are preserved.
	syncModifier := func(req *http.Request) {
		// Copy non-header fields.
		req.Method = origReq.Method
		req.Body = origReq.Body
		req.ContentLength = origReq.ContentLength
		req.TransferEncoding = origReq.TransferEncoding
		req.Close = origReq.Close
		req.Host = origReq.Host
		req.GetBody = origReq.GetBody
		req.URL = origReq.URL

		// Merge headers: origReq headers overwrite aoni defaults for the
		// same key, but aoni-only headers are preserved.
		maps.Copy(req.Header, origReq.Header)
	}

	cloned := t.client.Clone()

	// Preserve the original request's full URL path for relative resolution.
	// Only overwrite baseURL if the request URL has a valid Host, keeping
	// the client's configured baseURL for relative or schemeless paths.
	if origReq.URL.Host != "" {
		cloned.baseURL = &url.URL{
			Scheme: origReq.URL.Scheme,
			Host:   origReq.URL.Host,
		}
	}

	allMods := make([]RequestModifier, 0, 1+len(ctxMods))
	allMods = append(allMods, syncModifier)
	allMods = append(allMods, ctxMods...)

	resp, err := cloned.Request(
		origReq.Context(),
		origReq.Method,
		origReq.URL.RequestURI(),
		allMods...,
	)
	if err != nil {
		return nil, &url.Error{
			Op:  origReq.Method,
			URL: origReq.URL.String(),
			Err: err,
		}
	}

	return resp, nil
}

// Ensure AoniTransport implements http.RoundTripper.
var _ http.RoundTripper = (*Transport)(nil)
