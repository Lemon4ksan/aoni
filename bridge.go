// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"maps"
	"net/http"
	"net/url"

	"github.com/lemon4ksan/miyako/generic"
)

// ErrNilURL is returned when attempting to route an outbound HTTP request
// that does not specify a destination URL.
var ErrNilURL = errors.New("aoni bridge: request URL is nil")

// NewStdClient adapts an aoni [Client] into a standard [*http.Client].
//
// This client bridges third-party libraries (e.g., resty or custom API SDKs)
// with aoni's custom transport pipeline. It configures the underlying
// transport and intentionally disables the client's default cookie jar.
// This allows the internal aoni pipeline (such as [ProxyIsolatedCookieJar])
// to manage cookies internally, preventing double-cookie handling issues.
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
		Jar:       nil,
	}
}

// NewTransport constructs an [http.RoundTripper] (as a [*Transport]) configured
// to pass all outgoing requests through the provided aoni [Client].
//
// Swap this transport into an existing [*http.Client] to seamlessly inject
// aoni's advanced transport features (like JA4, happy eyeballs, and SSRF protection)
// without replacing the client itself.
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
}

// Transport implements the standard [http.RoundTripper] interface, intercepting
// outbound requests and delegating them to an active aoni [Client] pipeline.
type Transport struct {
	client *Client

	// BeforeRoundTrip runs immediately before a request enters the aoni engine.
	//
	// It provides a clone of the executing [Client] and the original request,
	// allowing dynamic modifications (e.g., adding headers or altering configuration)
	// on a per-request basis. The callback must return the final [Client] state.
	//
	// To prevent concurrent state sharing or race conditions, the modified client
	// is isolated strictly to the current request execution cycle.
	BeforeRoundTrip func(cloned *Client, origReq *http.Request) *Client
}

// RoundTrip satisfies [http.RoundTripper] by extracting request modifiers from
// the context, merging request metadata, and executing the request via aoni.
//
// Following standard specifications, all errors encountered
// during execution are returned wrapped in a [*url.Error], and the request body
// is guaranteed to be closed to prevent resource leaks.
//
// The incoming request's URL field must not be nil, otherwise [ErrNilURL] is returned.
func (t *Transport) RoundTrip(origReq *http.Request) (*http.Response, error) {
	if origReq.URL == nil {
		if origReq.Body != nil {
			_ = origReq.Body.Close()
		}

		return nil, &url.Error{
			Op:  origReq.Method,
			URL: "",
			Err: ErrNilURL,
		}
	}

	cloned := t.prepareClient(origReq)
	ctxMods := ContextModifiers(origReq.Context())

	modifiers := make([]RequestModifier, 0, 1+len(ctxMods))
	modifiers = append(modifiers, t.newSyncModifier(origReq))
	modifiers = append(modifiers, ctxMods...)

	resp, err := cloned.Request(
		origReq.Context(),
		origReq.Method,
		origReq.URL.RequestURI(),
		modifiers...,
	)
	if err != nil {
		return nil, t.wrapError(origReq, err)
	}

	return resp, nil
}

func (t *Transport) prepareClient(origReq *http.Request) *Client {
	cloned := t.client.Clone()

	if t.BeforeRoundTrip != nil {
		cloned = t.BeforeRoundTrip(cloned, origReq)
	}

	if origReq.URL.Host != "" {
		cloned.defaults.BaseURL = &url.URL{
			Scheme: generic.Coalesce(origReq.URL.Scheme, "https"),
			Host:   origReq.URL.Host,
		}
	}

	return cloned
}

func (t *Transport) newSyncModifier(origReq *http.Request) RequestModifier {
	return func(req Request) {
		aoniReq := req.HTTPRequest()
		if aoniReq == nil {
			req.SetMethod(origReq.Method)

			if origReq.URL != nil {
				req.SetURL(origReq.URL.String())
			}

			for k, vv := range origReq.Header {
				for _, v := range vv {
					req.AddHeader(k, v)
				}
			}

			return
		}

		resolvedURL := aoniReq.URL

		aoniReq.Method = origReq.Method
		aoniReq.Body = origReq.Body
		aoniReq.ContentLength = origReq.ContentLength
		aoniReq.TransferEncoding = origReq.TransferEncoding

		aoniReq.Close = origReq.Close
		if origReq.Host != "" {
			aoniReq.Host = origReq.Host
		}

		aoniReq.GetBody = origReq.GetBody

		if origReq.URL != nil {
			u := *origReq.URL
			if resolvedURL != nil {
				u.Scheme = resolvedURL.Scheme
				if resolvedURL.Host != "" {
					u.Host = resolvedURL.Host
				}
			}

			aoniReq.URL = &u
		}

		if aoniReq.Header == nil {
			aoniReq.Header = make(http.Header)
		}

		maps.Copy(aoniReq.Header, origReq.Header)
	}
}

func (t *Transport) wrapError(origReq *http.Request, err error) error {
	if origReq.Body != nil {
		_ = origReq.Body.Close()
	}

	bridgeErr := &BridgeError{
		Op:  origReq.Method,
		URL: origReq.URL.String(),
		Err: err,
		Metadata: map[string]any{
			"host":   origReq.URL.Host,
			"scheme": origReq.URL.Scheme,
		},
	}

	return &url.Error{
		Op:  origReq.Method,
		URL: origReq.URL.String(),
		Err: bridgeErr,
	}
}

// Ensure Transport satisfies the http.RoundTripper interface.
var _ http.RoundTripper = (*Transport)(nil)
