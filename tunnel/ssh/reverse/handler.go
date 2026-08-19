// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reverse

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type tcpipForwardMsg struct {
	Addr  string
	Rport uint32
}

type tcpipForwardSuccessReply struct {
	Port uint32
}

type forwardedTCPPayload struct {
	Addr       string
	Port       uint32
	OriginAddr string
	OriginPort uint32
}

// HandleGlobalRequests processes SSH global requests ("tcpip-forward", "cancel-tcpip-forward"),
// registering reverse tunnel routes on router and notifying the client of assigned ports/subdomains.
func HandleGlobalRequests(
	ctx context.Context,
	reqs <-chan *ssh.Request,
	sshConn *ssh.ServerConn,
	router *Router,
	onTunnelCreated func(t *Tunnel),
) {
	var activeTunnels sync.Map

	defer func() {
		activeTunnels.Range(func(key, _ any) bool {
			if host, ok := key.(string); ok {
				router.Unregister(host)
			}

			return true
		})
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-reqs:
			if !ok {
				return
			}

			dispatchGlobalRequest(req, sshConn, router, &activeTunnels, onTunnelCreated)
		}
	}
}

func dispatchGlobalRequest(
	req *ssh.Request,
	sshConn *ssh.ServerConn,
	router *Router,
	activeTunnels *sync.Map,
	onTunnelCreated func(t *Tunnel),
) {
	switch req.Type {
	case "tcpip-forward":
		var msg tcpipForwardMsg
		if err := ssh.Unmarshal(req.Payload, &msg); err != nil {
			_ = req.Reply(false, nil)
			return
		}

		tunnel, err := router.Register(sshConn, msg.Addr, msg.Rport)
		if err != nil {
			_ = req.Reply(false, nil)
			return
		}

		activeTunnels.Store(tunnel.Host, tunnel)

		replyPort := msg.Rport
		if replyPort == 0 {
			replyPort = 80
		}

		replyPayload := ssh.Marshal(tcpipForwardSuccessReply{Port: replyPort})
		_ = req.Reply(true, replyPayload)

		if onTunnelCreated != nil {
			onTunnelCreated(tunnel)
		}

	case "cancel-tcpip-forward":
		var msg tcpipForwardMsg
		if err := ssh.Unmarshal(req.Payload, &msg); err != nil {
			_ = req.Reply(false, nil)
			return
		}

		router.Unregister(msg.Addr)
		activeTunnels.Delete(msg.Addr)

		_ = req.Reply(true, nil)

	default:
		_ = req.Reply(false, nil)
	}
}

// OpenForwardedChannel opens a "forwarded-tcpip" SSH channel to the client for routing an incoming public connection.
func OpenForwardedChannel(tunnel *Tunnel, originAddr string, originPort uint32) (conn net.Conn, err error) {
	if tunnel == nil || tunnel.SSHConn == nil {
		return nil, ErrTunnelClosed
	}

	defer func() {
		if rec := recover(); rec != nil {
			conn = nil
			err = ErrTunnelClosed
		}
	}()

	host, portStr, pErr := net.SplitHostPort(originAddr)
	if pErr != nil {
		host = originAddr
		portStr = "0"
	}

	pVal, _ := strconv.ParseUint(portStr, 10, 32)
	if originPort > 0 {
		pVal = uint64(originPort)
	}

	payload := ssh.Marshal(forwardedTCPPayload{
		Addr:       tunnel.Subdomain,
		Port:       tunnel.Port,
		OriginAddr: host,
		OriginPort: uint32(pVal),
	})

	ch, reqs, openErr := tunnel.SSHConn.OpenChannel("forwarded-tcpip", payload)
	if openErr != nil {
		return nil, fmt.Errorf("aoni ssh reverse: open channel failed: %w", openErr)
	}

	go ssh.DiscardRequests(reqs)

	return &channelConnBridge{Channel: ch}, nil
}

type channelConnBridge struct {
	ssh.Channel
}

func (c *channelConnBridge) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80}
}

func (c *channelConnBridge) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80}
}

func (c *channelConnBridge) SetDeadline(_ time.Time) error {
	return nil
}

func (c *channelConnBridge) SetReadDeadline(_ time.Time) error {
	return nil
}

func (c *channelConnBridge) SetWriteDeadline(_ time.Time) error {
	return nil
}
