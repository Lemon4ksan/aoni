// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

const DefaultPingInterval = 3 * time.Second

// ClientOpts configures the HTTP/2 client multiplexer.
type ClientOpts struct {
	PingInterval time.Duration
	OnRTT        func(time.Duration)
}

// Context maps a fasthttp request/response pair to an asynchronous stream execution.
type Context struct {
	Request  *fasthttp.Request
	Response *fasthttp.Response
	Err      chan error
	StreamID uint32
}

// Client manages connection pooling and stream allocation for HTTP/2.
type Client struct {
	d           *Dialer
	onRTT       func(time.Duration)
	lck         sync.Mutex
	conns       list.List
	orderedKeys []string
}

// NewClient constructs an HTTP/2 Client instance using dialer and options.
func NewClient(d *Dialer, opts ClientOpts) *Client {
	return &Client{
		d:     d,
		onRTT: opts.OnRTT,
	}
}

// SetOrderedHeaders configures custom HPACK header ordering for anti-detect fingerprinting.
func (cl *Client) SetOrderedHeaders(keys []string) {
	cl.orderedKeys = keys
}

func (cl *Client) onConnectionDropped(c *Conn) {
	cl.lck.Lock()
	defer cl.lck.Unlock()

	for e := cl.conns.Front(); e != nil; e = e.Next() {
		if e.Value.(*Conn) == c {
			cl.conns.Remove(e)
			_, _, _ = cl.createConn()

			break
		}
	}
}

func (cl *Client) createConn() (*Conn, *list.Element, error) {
	c, err := cl.d.Dial(ConnOpts{
		PingInterval: cl.d.PingInterval,
		OnDisconnect: cl.onConnectionDropped,
	})
	if err != nil {
		return nil, nil, err
	}

	if len(cl.orderedKeys) > 0 {
		c.SetOrderedHeaders(cl.orderedKeys)
	}

	return c, cl.conns.PushFront(c), nil
}

// Do executes req over an available HTTP/2 stream and writes the result into res.
//
// Postconditions:
//   - Immediately cancels stream execution if ctx expires before completion.
func (cl *Client) Do(ctx context.Context, req *fasthttp.Request, res *fasthttp.Response) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	conn, err := cl.selectConn()
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	reqCtx := &Context{
		Request:  req,
		Response: res,
		Err:      errCh,
	}

	conn.Write(reqCtx)

	select {
	case <-ctx.Done():
		conn.CancelStream(reqCtx)
		return ctx.Err()

	case err := <-errCh:
		return err
	}
}

func (cl *Client) selectConn() (*Conn, error) {
	cl.lck.Lock()
	defer cl.lck.Unlock()

	var next *list.Element

	for e := cl.conns.Front(); e != nil; e = next {
		c := e.Value.(*Conn)
		next = e.Next()

		if c.Closed() {
			cl.conns.Remove(e)
			continue
		}

		if c.CanOpenStream() {
			return c, nil
		}
	}

	c, _, err := cl.createConn()

	return c, err
}
