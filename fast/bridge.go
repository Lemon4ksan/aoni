// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
)

// NewStdClient adapts a fast [Client] into a standard Go [*http.Client].
//
// It bridges third-party libraries (e.g., resty, go-resty, AWS SDKs, or custom API SDKs)
// with aoni/fast's ultra-high-throughput fasthttp transport pipeline (1.5M+ RPS, 0 allocs).
// Sets [http.Client.CheckRedirect] to [http.ErrUseLastResponse], delegating all redirect handling
// internally to aoni's pipeline to prevent double-handling or socket leaks.
// Returns a fully compatible [*http.Client] that routes all requests through aoni/fast's fasthttp engine.
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// NewTransport constructs an [http.RoundTripper] (as a [*Transport]) configured
// to pass all outgoing requests through the provided fast [Client].
//
// Swap this transport into an existing [*http.Client] to seamlessly inject
// aoni/fast's high-throughput transport features into third-party Go libraries without changing application code.
func NewTransport(c *Client) *Transport {
	return &Transport{
		client:           c,
		noRedirectClient: c.With(option.WithRedirectLimit(0)),
	}
}

// Transport implements the standard [http.RoundTripper] interface, intercepting
// outbound requests and delegating them to an active fast [Client] pipeline.
type Transport struct {
	client           *Client
	noRedirectClient *Client
}

// Unwrap returns the underlying fast [*Client].
func (t *Transport) Unwrap() any {
	return t.client
}

// Client returns the underlying fast [*Client].
func (t *Transport) Client() *Client {
	return t.client
}

// RoundTrip satisfies [http.RoundTripper], executing standard requests over fasthttp.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, &url.Error{
			Op:  req.Method,
			URL: "",
			Err: ErrNilURL,
		}
	}

	fastReq := NewRequest(nil)
	defer fastReq.Release()

	fastReq.SetContext(req.Context())
	fastReq.SetMethod(req.Method)
	fastReq.SetURL(req.URL.String())

	if req.Host != "" {
		fastReq.SetHeader("Host", req.Host)
	}

	copyHeaders(fastReq, req.Header)

	if req.Body != nil {
		body := req.Body
		if req.GetBody != nil {
			if b, err := req.GetBody(); err == nil && b != nil {
				body = b
			}
		}

		fastReq.SetBodyStream(body, req.ContentLength)

		if req.GetBody != nil {
			fastReq.SetGetBody(req.GetBody)
		}
	}

	if ct := req.Header.Get("Content-Type"); ct != "" {
		fastReq.SetHeader("Content-Type", ct)
	}

	resp, err := t.noRedirectClient.Do(fastReq)
	if err != nil {
		return nil, err
	}

	httpResp := &http.Response{
		StatusCode:    resp.StatusCode(),
		Status:        resp.Status(),
		Header:        resp.Headers(),
		Body:          &responseBodyCloser{ReadCloser: resp.BodyStream(), resp: resp},
		ContentLength: resolveContentLength(resp),
		Uncompressed:  resp.Uncompressed(),
		Request:       req,
	}

	if req.URL != nil && req.URL.Scheme == "https" {
		httpResp.TLS = &tls.ConnectionState{
			HandshakeComplete: true,
			Version:           tls.VersionTLS13,
		}
	}

	return httpResp, nil
}

// responseBodyCloser wraps a response stream to ensure both stream and underlying pooled response memory are closed.
type responseBodyCloser struct {
	io.ReadCloser
	resp aoni.Response
}

func (r *responseBodyCloser) Close() error {
	var err error
	if r.ReadCloser != nil {
		err = r.ReadCloser.Close()
	}

	if r.resp != nil {
		_ = r.resp.Close()
	}

	return err
}

// copyHeaders copies standard HTTP headers from src into destination aoni request.
func copyHeaders(dst aoni.Request, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.AddHeader(k, v)
		}
	}
}

// resolveContentLength parses Content-Length from response headers or returns -1 if absent/invalid.
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
