// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"crypto/tls"
	"errors"

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

	client.cfg = c
	return client
}

// Engine returns the underlying multi-protocol connection pool.
func (c *Client) Engine() *transport.Pool {
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
	fastReq, ok := req.(*Request)
	if !ok {
		return nil, errors.New("fast: invalid request type, expected *fast.Request")
	}

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

	return fastRes, nil
}

func (c *Client) execute(ctx context.Context, fastReq *machhttp.Request, fastRes *machhttp.Response) (map[string][]string, error, bool) {
	trailers, err := c.engine.DoCtx(ctx, fastReq, fastRes)
	return trailers, err, false
}
