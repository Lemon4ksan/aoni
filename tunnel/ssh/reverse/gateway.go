// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reverse

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
)

var ioBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// Gateway acts as a high-performance HTTP reverse proxy gateway, routing public web requests
// directly through reverse SSH channels to target developer machines.
type Gateway struct {
	Router *Router
}

// NewGateway creates a new HTTP reverse proxy [Gateway] backed by router.
func NewGateway(router *Router) *Gateway {
	return &Gateway{Router: router}
}

// ServeHTTP satisfies [http.Handler], matching request Host headers to active SSH tunnels.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	tunnel, ok := g.Router.Lookup(req.Host)
	if !ok {
		http.Error(w, "aoni reverse tunnel: 404 Tunnel Not Found", http.StatusNotFound)
		return
	}

	targetConn, err := OpenForwardedChannel(tunnel, req.RemoteAddr, 0)
	if err != nil {
		http.Error(w, "aoni reverse tunnel: 502 Bad Gateway (Tunnel Connection Failed)", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	if req.Header.Get("Upgrade") == "websocket" {
		g.proxyHijackedWebSocket(w, req, targetConn)
		return
	}

	if err := req.Write(targetConn); err != nil {
		http.Error(w, "aoni reverse tunnel: 502 Bad Gateway (Request Write Failed)", http.StatusBadGateway)
		return
	}

	resp, err := http.ReadResponse(bufioReaderFromConn(targetConn), req)
	if err != nil {
		http.Error(w, "aoni reverse tunnel: 502 Bad Gateway (Response Read Failed)", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	bufPtr := ioBufferPool.Get().(*[]byte)
	_, _ = io.CopyBuffer(w, resp.Body, *bufPtr)
	ioBufferPool.Put(bufPtr)
}

func (g *Gateway) proxyHijackedWebSocket(w http.ResponseWriter, req *http.Request, targetConn net.Conn) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "aoni reverse tunnel: WebSocket hijacking unsupported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "aoni reverse tunnel: WebSocket hijack failed", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	if err := req.Write(targetConn); err != nil {
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() { defer wg.Done(); proxyPipe(clientConn, targetConn) }()
	go func() { defer wg.Done(); proxyPipe(targetConn, clientConn) }()

	wg.Wait()
}

func proxyPipe(dst, src net.Conn) {
	bufPtr := ioBufferPool.Get().(*[]byte)
	_, _ = io.CopyBuffer(dst, src, *bufPtr)
	ioBufferPool.Put(bufPtr)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func bufioReaderFromConn(conn net.Conn) *bufio.Reader {
	return bufio.NewReaderSize(conn, 32*1024)
}
