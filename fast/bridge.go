// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"maps"
	"net/http"
	"net/url"
	"strconv"

	"github.com/lemon4ksan/aoni"
)

// NewStdClient adapts a fast [Client] into a standard [*http.Client].
//
// Bridges fasthttp with standard library HTTP abstractions.
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
	}
}

// NewTransport constructs an [http.RoundTripper] adapter backed by a fast [Client].
func NewTransport(c *Client) *Transport {
	return &Transport{client: c}
}

// Transport adapts a fast [Client] to satisfy the standard [http.RoundTripper] contract.
type Transport struct {
	client *Client
}

// RoundTrip satisfies [http.RoundTripper], executing standard requests over fasthttp.
//
// Postconditions:
//   - Request bodies are streamed directly without buffering full payloads in RAM.
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

	copyHeaders(fastReq, req.Header)

	if req.Body != nil {
		fastReq.SetBodyStream(req.Body, req.ContentLength)
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
		ContentLength: resolveContentLength(resp),
		Uncompressed:  resp.Uncompressed(),
		Request:       req,
	}

	maps.Copy(httpResp.Header, resp.Headers())

	return httpResp, nil
}

func copyHeaders(dst aoni.Request, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.AddHeader(k, v)
		}
	}
}

func resolveContentLength(resp aoni.Response) int64 {
	clStr := resp.Header("Content-Length")
	if clStr == "" {
		return -1
	}

	cl, err := strconv.ParseInt(clStr, 10, 64)
	if err != nil {
		return -1
	}

	return cl
}
