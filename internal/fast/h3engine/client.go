// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h3engine provides HTTP/3 client functionality using h1engine.
package h3engine

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/silicon/sysnet"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/internal/quic"
)

// Client manages connection pooling and multiplexing for HTTP/3 over QUIC.
type Client struct {
	mutex sync.Mutex
	conns map[string]*ClientConn

	TLSConfig  *tls.Config
	QUICConfig *quic.Config
	Settings   *Settings
}

// NewClient initializes a new HTTP/3 Client instance (RFC 9114 §3.1 & §3.2).
func NewClient(tlsCfg *tls.Config, quicCfg *quic.Config) *Client {
	if tlsCfg == nil {
		tlsCfg = &tls.Config{}
	}

	tlsConf := tlsCfg.Clone()
	// RFC 9114 §3.1 & §11.1: Application-Layer Protocol Negotiation (ALPN) token "h3"
	tlsConf.NextProtos = []string{"h3"}

	if quicCfg == nil {
		quicCfg = &quic.Config{
			EnableDatagrams: true,
		}
	}

	return &Client{
		conns:      make(map[string]*ClientConn),
		TLSConfig:  tlsConf,
		QUICConfig: quicCfg,
	}
}

// Do executes a h1engine.Request over HTTP/3 to the destination server, writing response into resp and returning trailers.
func (c *Client) Do(
	ctx context.Context,
	req *h1engine.Request,
	resp *h1engine.Response,
	headerOrder []string,
) (map[string][]string, error) {
	host := string(req.URI().Host())

	cc, err := c.getConn(ctx, host)
	if err != nil {
		return nil, err
	}

	trailers, err := cc.Do(ctx, req, resp, headerOrder)
	if err != nil {
		c.removeConn(host)
	}

	return trailers, err
}

// DoScoped executes a h1engine.Request over HTTP/3 with response body memory allocated from the provided borrow.Scope.
func (c *Client) DoScoped(
	ctx context.Context,
	req *h1engine.Request,
	resp *h1engine.Response,
	headerOrder []string,
	s *borrow.Scope,
) (map[string][]string, error) {
	host := string(req.URI().Host())

	cc, err := c.getConn(ctx, host)
	if err != nil {
		return nil, err
	}

	trailers, err := cc.DoScoped(ctx, req, resp, headerOrder, s)
	if err != nil {
		c.removeConn(host)
	}

	return trailers, err
}

// DoBatch executes a batch of requests concurrently over the multiplexed HTTP/3 QUIC connection.
func (c *Client) DoBatch(
	ctx context.Context,
	reqs []*h1engine.Request,
	resps []*h1engine.Response,
	headerOrder []string,
) error {
	if len(reqs) == 0 {
		return nil
	}

	if len(reqs) != len(resps) {
		return errors.New("h3engine: length of reqs and resps must match")
	}

	host := string(reqs[0].URI().Host())

	cc, err := c.getConn(ctx, host)
	if err != nil {
		return err
	}

	type result struct {
		idx int
		err error
	}

	resCh := make(chan result, len(reqs))

	for i := range reqs {
		go func(idx int) {
			_, err := cc.Do(ctx, reqs[idx], resps[idx], headerOrder)

			resCh <- result{idx: idx, err: err}
		}(i)
	}

	var firstErr error
	for range reqs {
		res := <-resCh
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}

	if firstErr != nil {
		c.removeConn(host)
	}

	return firstErr
}

func (c *Client) getConn(ctx context.Context, host string) (*ClientConn, error) {
	c.mutex.Lock()
	if conn, ok := c.conns[host]; ok {
		if !conn.isClosed() {
			c.mutex.Unlock()
			return conn, nil
		}

		delete(c.conns, host)
	}

	c.mutex.Unlock()

	tlsConf := c.TLSConfig.Clone()
	if tlsConf.ServerName == "" {
		hostOnly, _, err := net.SplitHostPort(host)
		if err == nil {
			tlsConf.ServerName = hostOnly
		} else {
			tlsConf.ServerName = host
		}
	}

	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}

	batchConn := sysnet.NewBatchUDPConn(udpConn)
	_ = batchConn.SetGSO(1200)
	_ = batchConn.SetGRO(true)

	tr := &quic.Transport{Conn: batchConn}

	udpAddr, err := net.ResolveUDPAddr("udp", host)
	if err != nil {
		_ = tr.Close()

		return nil, err
	}

	qConn, err := tr.Dial(ctx, udpAddr, tlsConf, c.QUICConfig)
	if err != nil {
		_ = tr.Close()

		return nil, err
	}

	cc, err := NewClientConn(qConn, c.Settings)
	if err != nil {
		return nil, err
	}

	c.mutex.Lock()
	c.conns[host] = cc
	c.mutex.Unlock()

	return cc, nil
}

func (c *Client) removeConn(host string) {
	c.mutex.Lock()
	delete(c.conns, host)
	c.mutex.Unlock()
}

// Close terminates all active HTTP/3 client connections in the pool.
func (c *Client) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for host, cc := range c.conns {
		_ = cc.Close()

		delete(c.conns, host)
	}

	return nil
}
