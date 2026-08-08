// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"crypto/tls"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
)

// NewStdClient adapts a fast [Client] into a standard [*http.Client].
func NewStdClient(c *Client) *http.Client {
	return &http.Client{
		Transport: NewTransport(c),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// NewTransport constructs an [http.RoundTripper] adapter backed by a fast [Client].
func NewTransport(c *Client) *Transport {
	return &Transport{
		client:           c,
		noRedirectClient: c.With(option.WithRedirectLimit(0)),
	}
}

// Transport adapts a fast [Client] to satisfy the standard [http.RoundTripper] contract.
type Transport struct {
	client           *Client
	noRedirectClient *Client
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

	resp, err := t.noRedirectClient.Do(fastReq)
	if err != nil {
		return nil, err
	}

	httpResp := &http.Response{
		StatusCode:    resp.StatusCode(),
		Status:        resp.Status(),
		Header:        make(http.Header),
		Body:          &responseBodyCloser{ReadCloser: resp.BodyStream(), resp: resp},
		ContentLength: resolveContentLength(resp),
		Uncompressed:  resp.Uncompressed(),
		Request:       req,
	}

	maps.Copy(httpResp.Header, resp.Headers())

	if req.URL != nil && req.URL.Scheme == "https" {
		httpResp.TLS = &tls.ConnectionState{
			HandshakeComplete: true,
			Version:           tls.VersionTLS13,
		}
	}

	return httpResp, nil
}

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
