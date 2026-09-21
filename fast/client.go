// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"

	machhttp "github.com/lemon4ksan/mach/proto/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/transport"
)

type Client struct {
	engine *transport.Pool
	cfg    aoni.Config
}

// NewClient creates a new high-performance baremetal fast.Client.
// It accepts both [fast.Option] and [aoni.ClientOption].
func NewClient(opts ...any) *Client {
	c := aoni.Config{}
	client := &Client{
		engine: transport.NewPool(),
	}

	for _, opt := range opts {
		switch o := opt.(type) {
		case aoni.ClientOption:
			o(&c)
		case Option:
			o(client)
		case func(*Client):
			o(client)
		}
	}

	if c.Ext.WrapTLSClient != nil {
		client.engine.WrapTLSClient = c.Ext.WrapTLSClient
	}

	client.cfg = c

	return client
}

// Engine returns the underlying multi-protocol connection pool.
func (c *Client) Engine() *transport.Pool {
	return c.engine
}

// Unwrap returns the underlying multi-protocol connection pool.
func (c *Client) Unwrap() *transport.Pool {
	return c.engine
}

// SetTLSConfig configures the TLS configuration on the underlying transport pool.
func (c *Client) SetTLSConfig(tlsConf *tls.Config) *Client {
	c.engine.TLSConfig = tlsConf
	return c
}

// EnableH2 enables or disables HTTP/2 in the underlying transport pool.
func (c *Client) EnableH2(enable bool) *Client {
	c.engine.EnableH2 = enable
	return c
}

// EnableH3 enables or disables HTTP/3 QUIC in the underlying transport pool.
func (c *Client) EnableH3(enable bool) *Client {
	c.engine.EnableH3 = enable
	return c
}

func (c *Client) Do(req core.Request) (core.Response, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	fastReq, cleanup := toFastRequest(req)
	defer cleanup()

	fastRes := NewResponse(nil)

	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	trailers, err, autoReleased := c.executeWithRedirects(ctx, fastReq.req, fastRes.resp)
	if err != nil {
		if !autoReleased {
			fastRes.Release()
		}

		return nil, err
	}

	fastRes.SetTrailers(trailers)
	decompressFastResponse(fastRes.resp)

	return fastRes, nil
}

func (c *Client) execute(
	ctx context.Context,
	fastReq *machhttp.Request,
	fastRes *machhttp.Response,
) (map[string][]string, error, bool) {
	trailers, err := c.engine.DoCtx(ctx, fastReq, fastRes)

	return trailers, err, false
}

// AcquireRequest obtains a pooled [Request] instance.
func (c *Client) AcquireRequest() core.Request {
	return NewRequest(nil)
}

// ReleaseRequest releases a pooled [Request] instance back to the memory pool.
func (c *Client) ReleaseRequest(req core.Request) {
	ReleaseRequest(req)
}

// ReleaseResponse releases a pooled [Response] instance back to the memory pool.
func (c *Client) ReleaseResponse(res core.Response) {
	ReleaseResponse(res)
}

// ReleaseRequest releases a request adapter back to the pool.
func ReleaseRequest(req core.Request) {
	if r, ok := req.(*Request); ok {
		r.Release()
	}
}

// ReleaseResponse releases a response adapter back to the pool.
func ReleaseResponse(res core.Response) {
	if r, ok := res.(*Response); ok {
		r.Release()
	}
}

func toFastRequest(req core.Request) (*Request, func()) {
	if fastReq, ok := req.(*Request); ok {
		return fastReq, func() {}
	}

	fastReq := NewRequest(nil)
	fastReq.SetContext(req.Context())
	fastReq.SetMethod(req.Method())

	rawURL := req.URL()
	if rawQuery := req.RawQuery(); rawQuery != "" && !strings.Contains(rawURL, "?") {
		rawURL += "?" + rawQuery
	}
	fastReq.SetURL(rawURL)

	if httpReq := req.HTTPRequest(); httpReq != nil {
		for k, vv := range httpReq.Header {
			for _, v := range vv {
				fastReq.AddHeader(k, v)
			}
		}
		if httpReq.Host != "" {
			fastReq.req.Header.SetHost(httpReq.Host)
		}
		if httpReq.Body != nil && httpReq.Body != http.NoBody {
			fastReq.SetBodyStream(httpReq.Body, httpReq.ContentLength)
			if httpReq.GetBody != nil {
				fastReq.SetGetBody(httpReq.GetBody)
			}
		}
	} else {
		for k, v := range req.Headers() {
			fastReq.AddHeaderBytes(k, v)
		}
		if b := req.BodyBytes(); len(b) > 0 {
			fastReq.SetBodyBytes(b)
		} else if stream := req.BodyStream(); stream != nil {
			fastReq.SetBodyStream(stream, -1)
		}
	}

	fastReq.SetConfig(req.Config())

	return fastReq, func() {
		fastReq.Release()
	}
}
