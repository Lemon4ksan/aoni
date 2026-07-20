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

// NewStdClient returns a standard [http.Client] whose transport routes all
// requests through the configured aoni [Client] pipeline.
//
// The returned client has Jar set to nil to avoid double cookie handling.
// The aoni [ProxyIsolatedCookieJar] manages cookies internally.
//
// Usage:
//
//	client := NewClient(nil,
//	    option.WithTLSFingerprint(BrowserChrome),
//	    option.WithDoHResolver(),
//	)
//	stdClient := NewStdClient(client)
//
//	// Use with any third-party library
//	restyClient.SetHTTPClient(stdClient)
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
		Jar:       nil,
	}
}

// NewTransport returns a new [http.RoundTripper] (specifically [Transport])
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
//	ctx := WithContextModifier(context.Background(),
//	    WithHeader("X-Api-Key", "secret"),
//	    TraceJA4(info),
//	)
//	resp, err := restyClient.R().SetContext(ctx).Get("/api/data")
func WithContextModifier(ctx context.Context, mods ...RequestModifier) context.Context {
	if len(mods) == 0 {
		return ctx
	}

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		cfg = &RequestConfig{
			Metadata: make(map[string]any),
		}
		ctx = context.WithValue(ctx, requestConfigKey{}, cfg)
	}

	cfg.Modifiers = append(cfg.Modifiers, mods...)

	return ctx
}

// ContextModifiers extracts the RequestModifiers previously stored via
// [WithContextModifier]. Returns nil if none are present.
func ContextModifiers(ctx context.Context) []RequestModifier {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		return cfg.Modifiers
	}

	return nil
}

// WithTraceContext returns a [RequestModifier] that attaches a new [TraceInfo]
// to the request context. This allows developers to retrieve network
// timing and JA4/JA4H fingerprints using [ResponseTrace] after the request finishes.
func WithTraceContext() RequestModifier {
	return func(req *http.Request) {
		info := &TraceInfo{}
		getOrInitRequestConfig(req).TraceInfo = info
		TraceJA4(info)(req)
	}
}

// ResponseTrace extracts the [TraceInfo] previously captured via [WithTraceContext].
// Returns nil if no trace was registered on the request.
func ResponseTrace(resp *http.Response) *TraceInfo {
	if resp == nil || resp.Request == nil {
		return nil
	}

	cfg := GetRequestConfig(resp.Request.Context())
	if cfg != nil {
		return cfg.TraceInfo
	}

	return nil
}

// Transport implements [http.RoundTripper] by routing requests through
// a configured aoni [Client] pipeline.
type Transport struct {
	client *Client

	// BeforeRoundTrip is an optional lifecycle hook executed immediately before
	// the request is dispatched through the aoni engine.
	//
	// It receives the cloned, pre-configured [Client] and the original request,
	// and must return the final [Client] to be used. This allows flexible,
	// dynamic transport-level adjustments (such as adding headers, configuring
	// authentication, or overriding client settings dynamically).
	//
	// Any changes made to cloned Client within this hook should not result in state sharing
	// between goroutines, as the returned client will only be used for one specific Request().
	BeforeRoundTrip func(cloned *Client, origReq *http.Request) *Client
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

		maps.Copy(req.Header, origReq.Header)
	}

	cloned := t.client.Clone()

	// Apply the lifecycle hook if registered
	if t.BeforeRoundTrip != nil {
		cloned = t.BeforeRoundTrip(cloned, origReq)
	}

	// Preserve the original request's full URL path for relative resolution.
	// Only overwrite baseURL if the request URL has a valid Host, keeping
	// the client's configured baseURL for relative or schemeless paths.
	if origReq.URL.Host != "" {
		cloned.defaults.BaseURL = &url.URL{
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
		bErr := &BridgeError{
			Op:       origReq.Method,
			URL:      origReq.URL.String(),
			Err:      err,
			Metadata: make(map[string]any),
		}
		if origReq.URL != nil {
			bErr.Metadata["host"] = origReq.URL.Host
			bErr.Metadata["scheme"] = origReq.URL.Scheme
		}

		return nil, &url.Error{
			Op:  origReq.Method,
			URL: origReq.URL.String(),
			Err: bErr,
		}
	}

	return resp, nil
}

// Ensure AoniTransport implements http.RoundTripper.
var _ http.RoundTripper = (*Transport)(nil)
