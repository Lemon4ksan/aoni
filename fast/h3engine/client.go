// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"context"
	"crypto/tls"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
	"github.com/valyala/fasthttp"
)

// Client manages connection pooling and multiplexing for HTTP/3 over QUIC.
type Client struct {
	mutex sync.Mutex
	conns map[string]*ClientConn

	TLSConfig  *tls.Config
	QUICConfig *quic.Config
	Settings   *Settings
}

// NewClient initializes a new HTTP/3 Client instance.
func NewClient(tlsCfg *tls.Config, quicCfg *quic.Config) *Client {
	if tlsCfg == nil {
		tlsCfg = &tls.Config{}
	}

	tlsConf := tlsCfg.Clone()
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

// Do executes a fasthttp.Request over HTTP/3 to the destination server, writing response into resp.
//
// Postconditions:
//   - Automatically purges closed QUIC connections and reconnects on connection loss.
func (c *Client) Do(ctx context.Context, req *fasthttp.Request, resp *fasthttp.Response, headerOrder []string) error {
	host := string(req.URI().Host())

	cc, err := c.getConn(ctx, host)
	if err != nil {
		return err
	}

	err = cc.Do(ctx, req, resp, headerOrder)
	if err != nil {
		c.removeConn(host)
	}

	return err
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

	qConn, err := quic.DialAddr(ctx, host, tlsConf, c.QUICConfig)
	if err != nil {
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
