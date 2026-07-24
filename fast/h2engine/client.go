// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h2engine provides an HTTP/2 client multiplexer.
package h2engine

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
)

const DefaultPingInterval = 3 * time.Second

type streamState int32

const (
	streamIdle streamState = iota
	streamOpen
	streamHalfClosed
	streamClosed
)

// ClientOpts configures the HTTP/2 client multiplexer.
type ClientOpts struct {
	PingInterval time.Duration
	OnRTT        func(time.Duration)
}

// Context maps a fasthttp request/response pair to an asynchronous stream execution.
type Context struct {
	Request       *fasthttp.Request
	Response      *fasthttp.Response
	Err           chan error
	Trailers      map[string][]string
	StreamID      uint32
	streamWindow  int32
	state         atomic.Int32
	headersParsed bool
}

// State yields current lifecycle state of the HTTP/2 stream.
func (ctx *Context) State() streamState {
	return streamState(ctx.state.Load())
}

// SetState updates lifecycle state of the HTTP/2 stream.
func (ctx *Context) SetState(s streamState) {
	ctx.state.Store(int32(s))
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

// Do executes req over an available HTTP/2 stream, automatically retrying
// on a fresh connection if affected by a graceful GOAWAY frame.
//
// Postconditions:
//   - Retries transparently up to 3 times on new connections when GOAWAY is received.
func (cl *Client) Do(ctx context.Context, req *fasthttp.Request, res *fasthttp.Response) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	for range 3 {
		err := cl.doOnce(ctx, req, res)
		if errors.Is(err, ErrGoAwayRetryable) {
			continue
		}

		return err
	}

	return ErrGoAwayRetryable
}

// DoWithTrailers executes req over an available HTTP/2 stream and returns captured response trailers.
func (cl *Client) DoWithTrailers(ctx context.Context, req *fasthttp.Request, res *fasthttp.Response) (map[string][]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for range 3 {
		trailers, err := cl.doOnceWithTrailers(ctx, req, res)
		if errors.Is(err, ErrGoAwayRetryable) {
			continue
		}

		return trailers, err
	}

	return nil, ErrGoAwayRetryable
}

func (cl *Client) doOnceWithTrailers(ctx context.Context, req *fasthttp.Request, res *fasthttp.Response) (map[string][]string, error) {
	conn, err := cl.selectConn()
	if err != nil {
		return nil, err
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
		return nil, ctx.Err()

	case err := <-errCh:
		return reqCtx.Trailers, err
	}
}

func (cl *Client) doOnce(ctx context.Context, req *fasthttp.Request, res *fasthttp.Response) error {
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

// selectConn selects an available connection from pool with Late-Binding optimization.
func (cl *Client) selectConn() (*Conn, error) {
	cl.lck.Lock()
	defer cl.lck.Unlock()

	for {
		if conn := cl.findAvailableConnLocked(); conn != nil {
			return conn, nil
		}

		c, err := cl.dialOrWaitLateBindingLocked()
		if err != nil {
			return nil, err
		}

		if c != nil {
			return c, nil
		}
	}
}

func (cl *Client) findAvailableConnLocked() *Conn {
	var next *list.Element

	for e := cl.conns.Front(); e != nil; e = next {
		c := e.Value.(*Conn)
		next = e.Next()

		if c.Closed() {
			cl.conns.Remove(e)
			continue
		}

		if c.CanOpenStream() {
			return c
		}
	}

	return nil
}

func (cl *Client) dialOrWaitLateBindingLocked() (*Conn, error) {
	// Release pool lock during network dial to avoid blocking sibling requests
	cl.lck.Unlock()
	c, _, err := cl.createConn()
	cl.lck.Lock()

	if err != nil {
		return nil, err
	}

	// Late-Binding Check: If an existing warm connection freed a stream slot during dial, bind to it!
	if existing := cl.findAvailableConnLocked(); existing != nil && existing != c {
		return existing, nil
	}

	return c, nil
}
