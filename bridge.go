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

// CloneHTTPClient returns a deep copy of an [*http.Client] and its transport layers.
//
// It recursively clones [http.RoundTripper] decorators implementing [TransportCloner] and [TransportUnwrapper].
// The base [*http.Transport] is duplicated via Clone() to allocate independent connection pools,
// idle socket caches, and TLS configurations, preventing race conditions on concurrent modifications.
//
// DANGER: If a decorator implements neither [TransportCloner] nor [TransportUnwrapper], it is copied by reference.
// This leads to shared mutable state and connection pool collisions across client instances.
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

// Std returns an adapted standard library [*http.Client] backed by this Client's pipeline.
// Outgoing requests executed via this client pass through the entire aoni pipeline.
func (c *Client) Std() *http.Client {
	return NewStdClient(c)
}

// NewTransport constructs an [http.RoundTripper] (as a [*Transport]) configured
// to route all outgoing requests through the provided aoni [Client] pipeline.
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
}

// Transport implements the standard [http.RoundTripper] interface.
// It intercepts outbound requests from standard library clients and routes them through
// the provided [Client] pipeline.
//
// DANGER: If a request specifies a host without a scheme (req.URL.Host != "" && req.URL.Scheme == ""),
// Transport automatically normalizes the scheme to "https" in accordance with RFC 7230 Section 2.7.2
// to prevent routing failures. This allocates a cloned request.
type Transport struct {
	client *Client

	// BeforeRoundTrip is invoked before a request enters the pipeline.
	// It allows dynamic per-request client cloning and modifier injection.
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
