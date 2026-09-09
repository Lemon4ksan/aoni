// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/url"
)

// TransportUnwrapper is implemented by RoundTripper decorators that wrap an inner RoundTripper.
type TransportUnwrapper interface {
	Unwrap() http.RoundTripper
}

// TransportCloner is implemented by RoundTripper decorators capable of creating a deep-copy
// of themselves around a newly cloned inner RoundTripper.
type TransportCloner interface {
	CloneTransport(next http.RoundTripper) http.RoundTripper
}

// CloneHTTPClient produces a deep, memory-isolated copy of an [*http.Client] and its nested transport layers.
//
// In standard Go, performing `*c = *parent` leaves the underlying [http.Transport] and TLS configurations
// shared, resulting in socket pool collisions and race hazards when modifying options concurrently.
//
// CloneHTTPClient recursively traverses nested decorator layers (via [TransportCloner] and [TransportUnwrapper]),
// cloning base [*http.Transport] instances ([http.Transport.Clone]) and TLS configurations to ensure
// that the cloned client is completely decoupled from the original.
// If c is nil, returns nil.
func CloneHTTPClient(c *http.Client) *http.Client {
	if c == nil {
		return nil
	}

	cloned := *c
	if cloned.Transport == nil {
		return &cloned
	}

	cloned.Transport = cloneRoundTripper(cloned.Transport)

	return &cloned
}

func cloneRoundTripper(tr http.RoundTripper) http.RoundTripper {
	if tr == nil {
		return nil
	}

	if cloner, ok := tr.(TransportCloner); ok {
		if unwrapper, ok := tr.(TransportUnwrapper); ok {
			nextCloned := cloneRoundTripper(unwrapper.Unwrap())
			return cloner.CloneTransport(nextCloned)
		}
	}

	if unwrapper, ok := tr.(TransportUnwrapper); ok {
		return cloneRoundTripper(unwrapper.Unwrap())
	}

	if baseTr, ok := tr.(*http.Transport); ok && baseTr != nil {
		return baseTr.Clone()
	}

	return tr
}

// NewStdClient adapts an aoni [Client] into a standard [*http.Client].
// Outgoing requests executed via this client pass through the entire aoni pipeline,
// including TLS fingerprinting, proxy rotators, and retries.
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
		Jar:       nil,
	}
}

// NewTransport constructs an [http.RoundTripper] (as a [*Transport]) configured
// to route all outgoing requests through the provided aoni [Client] pipeline.
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
}

// Transport implements the standard [http.RoundTripper] interface, intercepting
// outbound requests from standard library consumers (e.g. cloud SDKs, third-party clients)
// and executing them through an active aoni [Client] pipeline.
//
// Automatic URL & Scheme Correction:
// If a request specifies a host without a scheme (e.g. `req.URL.Host != "" && req.URL.Scheme == ""`),
// Transport automatically normalizes the scheme to "https" to prevent routing failures.
type Transport struct {
	client *Client

	// BeforeRoundTrip is an optional interceptor hook invoked immediately before a request
	// enters the aoni pipeline, allowing dynamic per-request client cloning and modifier injection.
	BeforeRoundTrip func(cloned *Client, origReq *http.Request) *Client
}

// Unwrap returns the underlying aoni [*Client].
func (t *Transport) Unwrap() *Client {
	return t.client
}

// RoundTrip satisfies [http.RoundTripper] by executing requests through the aoni pipeline.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		closeReqBody(req)

		var op string
		if req != nil {
			op = req.Method
		}

		return nil, &url.Error{
			Op:  op,
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

func (t *Transport) wrapError(req *http.Request, err error) error {
	closeReqBody(req)

	reqURL := req.URL.String()
	bridgeErr := &BridgeError{
		Op:  req.Method,
		URL: reqURL,
		Err: err,
		Metadata: map[string]any{
			"host":   req.URL.Host,
			"scheme": req.URL.Scheme,
		},
	}

	return &url.Error{
		Op:  req.Method,
		URL: reqURL,
		Err: bridgeErr,
	}
}

func closeReqBody(req *http.Request) {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
}

var _ http.RoundTripper = (*Transport)(nil)
