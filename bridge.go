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
//
// See also [BridgeError] and [Transport.RoundTrip].
var ErrNilURL = errors.New("aoni/bridge: request URL is nil")

// NewStdClient adapts an aoni [Client] into a standard [*http.Client].
//
// This client bridges third-party libraries (e.g., resty or custom API SDKs)
// with aoni's custom transport pipeline. It configures the underlying
// transport and intentionally disables the client's default cookie jar
// on the stdlib client wrapper, allowing aoni's internal pipeline (such as
// [ProxyIsolatedJar]) to manage cookies internally without double-handling issues.
//
// The returned client is safe for concurrent use by multiple goroutines.
// If c is nil, execution behavior is undefined.
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
//
// The returned [*Transport] is safe for concurrent use by multiple goroutines.
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
}

// Transport implements the standard [http.RoundTripper] interface, intercepting
// outbound requests and delegating them to an active aoni [Client] pipeline.
//
// Transport instances are safe for concurrent use by multiple goroutines.
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

// RoundTrip satisfies [http.RoundTripper] by executing requests through the aoni pipeline.
//
// If req or req.URL is nil, RoundTrip closes req.Body (if present) and returns a [*url.Error] wrapping [ErrNilURL].
//
// Following the [http.RoundTripper] contract, RoundTrip never mutates the incoming req or its URL.
// If req.URL.Scheme is missing, an internal clone is created. On any execution error, req.Body is guaranteed to be closed.
//
// RoundTrip is safe for concurrent use by multiple goroutines.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		closeReqBody(req)

		op := ""
		if req != nil {
			op = req.Method
		}

		return nil, &url.Error{
			Op:  op,
			URL: "",
			Err: ErrNilURL,
		}
	}

	if req.URL.Host != "" && req.URL.Scheme == "" {
		u := *req.URL
		u.Scheme = "https"
		reqClone := req.Clone(req.Context())
		reqClone.URL = &u
		req = reqClone
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

// wrapError constructs a standardized [*url.Error] containing a [*BridgeError] metadata container
// and guarantees that the original request body is closed.
func (t *Transport) wrapError(origReq *http.Request, err error) error {
	closeReqBody(origReq)

	reqURL := origReq.URL.String()
	bridgeErr := &BridgeError{
		Op:  origReq.Method,
		URL: reqURL,
		Err: err,
		Metadata: map[string]any{
			"host":   origReq.URL.Host,
			"scheme": origReq.URL.Scheme,
		},
	}

	return &url.Error{
		Op:  origReq.Method,
		URL: reqURL,
		Err: bridgeErr,
	}
}

func closeReqBody(req *http.Request) {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
}

// Ensure Transport satisfies the http.RoundTripper interface.
var _ http.RoundTripper = (*Transport)(nil)
