// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type (
	modsCtxKey      struct{}
	traceInfoCtxKey struct{}
)

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

// TraceContext returns a [RequestModifier] that attaches a new [TraceInfo]
// to the request context. This allows developers to retrieve network
// timing and JA4/JA4H fingerprints using [ResponseTrace] after the request finishes.
func TraceContext() RequestModifier {
	return func(req *http.Request) {
		info := &TraceInfo{}
		ctx := context.WithValue(req.Context(), traceInfoCtxKey{}, info)
		*req = *req.WithContext(ctx)

		Trace(info)(req)
		TraceJA4(info)(req)
	}
}

// ResponseTrace extracts the [TraceInfo] previously captured via [TraceContext].
// Returns nil if no trace was registered on the request.
func ResponseTrace(resp *http.Response) *TraceInfo {
	if resp == nil || resp.Request == nil {
		return nil
	}

	info, _ := resp.Request.Context().Value(traceInfoCtxKey{}).(*TraceInfo)

	return info
}

// BridgeError represents an error occurring during standard-client bridging.
// It implements the standard error interface and can be unwrapped to retrieve
// the underlying client or transport errors.
type BridgeError struct {
	Op       string
	URL      string
	Err      error
	Metadata map[string]any
}

// Error implements the standard error interface.
func (e *BridgeError) Error() string {
	return fmt.Sprintf("aoni bridge: %s %s: %v", e.Op, e.URL, e.Err)
}

// Unwrap returns the underlying wrapped error.
func (e *BridgeError) Unwrap() error {
	return e.Err
}

// Transport implements [http.RoundTripper] by routing requests through
// a configured aoni [Client] pipeline.
//
// # Custom X-Aoni Headers
//
// Transport supports declarative configuration of outbound requests via
// specialized request headers. This provides a clean way to integrate
// advanced transport features (like TLS fingerprints, proxy routing, or security
// policies) into standard HTTP clients or third-party SDKs (such as pocketbase-go,
// Supabase, or Resty) that allow header customization but lack direct access to
// context modifiers.
//
// These internal headers are parsed dynamically during RoundTrip, applied
// to the cloned request's Client configuration, and fully stripped from the outgoing
// request. The remote server will never see these configuration headers.
//
// Supported headers:
//
//   - X-Aoni-Proxy: Sets the proxy URL for the current request.
//     Format: A standard URL string (e.g. "http://user:pass@127.0.0.1:8080" or "socks5://127.0.0.1:1080").
//
//   - X-Aoni-TLS-Fingerprint: Selects a uTLS browser client profile to bypass passive TLS fingerprinting.
//     Values: "chrome", "firefox", "safari", "none".
//
//   - X-Aoni-Timeout: Sets a request-specific timeout.
//     Format: A duration string parsable by time.ParseDuration (e.g. "10s", "500ms").
//
//   - X-Aoni-SSRF-Guard: Protects the application by blocking connection attempts to private and loopback IPs.
//     Values: "true", "1".
//
//   - X-Aoni-Max-Response-Size: Protects against decompression bombs by setting a maximum response body size limit.
//     Format: Integer size in bytes (e.g. "1048576" for 1 MB).
type Transport struct {
	client *Client

	// BeforeRoundTrip is an optional lifecycle hook executed immediately before
	// the request is dispatched through the aoni engine.
	// It receives the cloned, pre-configured [Client] and the original request,
	// and must return the final [Client] to be used. This allows flexible,
	// dynamic transport-level adjustments (such as adding headers, configuring
	// authentication, or overriding client settings dynamically).
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

		// Merge headers: origReq headers overwrite aoni defaults for the
		// same key, but aoni-only headers are preserved.
		maps.Copy(req.Header, origReq.Header)

		// Strip any X-Aoni- headers from the outgoing request to avoid leak
		for k := range req.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-aoni-") {
				req.Header.Del(k)
			}
		}
	}

	cloned := t.client.Clone()

	// Parse any special custom headers to configure the client options on the fly.
	for k, vs := range origReq.Header {
		if len(vs) == 0 {
			continue
		}

		val := vs[0]
		switch strings.ToLower(k) {
		case "x-aoni-proxy":
			if u, err := url.Parse(val); err == nil {
				cloned = cloned.WithProxy(u)
			}
		case "x-aoni-tls-fingerprint":
			var browser BrowserID
			switch strings.ToLower(val) {
			case "chrome":
				browser = BrowserChrome
			case "firefox":
				browser = BrowserFirefox
			case "safari":
				browser = BrowserSafari
			case "none":
				browser = BrowserNone
			}

			cloned = cloned.WithTLSFingerprint(browser)

		case "x-aoni-timeout":
			if d, err := time.ParseDuration(val); err == nil {
				cloned = cloned.WithTimeout(d)
			}
		case "x-aoni-ssrf-guard":
			if val == "true" || val == "1" {
				cloned = cloned.WithSSRFGuard()
			}
		case "x-aoni-max-response-size":
			if size, err := strconv.ParseInt(val, 10, 64); err == nil {
				cloned = cloned.WithMaxResponseSize(size)
			}
		}
	}

	// Apply the lifecycle hook if registered
	if t.BeforeRoundTrip != nil {
		cloned = t.BeforeRoundTrip(cloned, origReq)
	}

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
