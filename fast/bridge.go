// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"errors"
	stdio "io"
	"maps"
	"net/http"
	"net/url"
)

var (
	// ErrNilURL is returned when attempting to route an outbound HTTP request without a destination URL.
	ErrNilURL = errors.New("aoni fast bridge: request URL is nil")
)

// NewStdClient adapts a [Client] (fasthttp) into a standard [*http.Client].
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
	}
}

// NewTransport wraps c into an [http.RoundTripper] backed by fasthttp.
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
}

// Transport implements [http.RoundTripper] delegating execution to a high-performance fasthttp [Client].
type Transport struct {
	client *Client
}

// RoundTrip satisfies [http.RoundTripper], executing standard [*http.Request] instances over fasthttp.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, &url.Error{
			Op:  req.Method,
			URL: "",
			Err: ErrNilURL,
		}
	}

	fastReq := NewRequest(nil)
	fastReq.SetContext(req.Context())
	fastReq.SetMethod(req.Method)
	fastReq.SetURL(req.URL.String())

	if req.Header != nil {
		for k, vv := range req.Header {
			for _, v := range vv {
				fastReq.AddHeader(k, v)
			}
		}
	}

	if req.Body != nil {
		b, err := stdio.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}

		fastReq.SetBodyBytes(b)
	}

	resp, err := t.client.Do(fastReq)
	if err != nil {
		return nil, err
	}

	httpResp := &http.Response{
		StatusCode:    resp.StatusCode(),
		Status:        resp.Status(),
		Header:        make(http.Header),
		Body:          resp.BodyStream(),
		ContentLength: int64(len(resp.BodyBytes())),
		Request:       req,
	}

	maps.Copy(httpResp.Header, resp.Headers())

	return httpResp, nil
}
