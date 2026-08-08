// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"net/http"
	"net/url"
)

// ErrNilURL is returned when attempting to route an outbound HTTP request
// that does not specify a destination URL.
var ErrNilURL = errors.New("aoni bridge: request URL is nil")

// NewStdClient adapts an aoni [Client] into a standard [*http.Client].
//
// This client bridges third-party libraries (e.g., resty or custom API SDKs)
// with aoni's custom transport pipeline. It configures the underlying
// transport and intentionally disables the client's default cookie jar
// on the stdlib client wrapper, allowing aoni's internal pipeline (such as
// [ProxyIsolatedJar]) to manage cookies internally without double-handling issues.
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
	// allowing dynamic modifications (e.g., altering TLS configuration or proxies)
	// on a per-request basis. The callback must return the modified [Client].
	//
	// Fast Path Optimization:
	// If BeforeRoundTrip is nil, the request executes directly on the shared client
	// without memory allocations or client cloning.
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
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}

		return nil, &url.Error{
			Op:  req.Method,
			URL: "",
			Err: ErrNilURL,
		}
	}

	if req.URL.Host != "" && req.URL.Scheme == "" {
		req.URL.Scheme = "https"
	}

	activeClient := t.client
	if t.BeforeRoundTrip != nil {
		activeClient = t.BeforeRoundTrip(t.client.Clone(), req)
	}

	resp, err := activeClient.HTTP().Do(req)
	if err != nil {
		return nil, t.wrapError(req, err)
	}

	return resp, nil
}

func (t *Transport) wrapError(origReq *http.Request, err error) error {
	if origReq.Body != nil {
		_ = origReq.Body.Close()
	}

	var reqURL, host, scheme string
	if origReq.URL != nil {
		reqURL = origReq.URL.String()
		host = origReq.URL.Host
		scheme = origReq.URL.Scheme
	}

	bridgeErr := &BridgeError{
		Op:  origReq.Method,
		URL: reqURL,
		Err: err,
		Metadata: map[string]any{
			"host":   host,
			"scheme": scheme,
		},
	}

	return &url.Error{
		Op:  origReq.Method,
		URL: reqURL,
		Err: bridgeErr,
	}
}

// Ensure Transport satisfies the http.RoundTripper interface.
var _ http.RoundTripper = (*Transport)(nil)
