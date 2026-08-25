// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
)

var (
	// ErrNoRouteToHost is returned when an outbound packet has no matching client session or route.
	ErrNoRouteToHost = errors.New("aoni/masque: no route to host")

	// ErrServerClosed is returned when operations are performed on a closed server.
	ErrServerClosed = errors.New("aoni/masque: server is closed")
)

// ServerConfig configures a MASQUE proxying server.
type ServerConfig struct {
	IPv4Prefix    netip.Prefix
	IPv6Prefix    netip.Prefix
	MaxMTU        int
	AuthFunc      func(username, password string) bool
	PacketHandler func(packet []byte, clientIP netip.Addr, reply func(packet []byte) error) error
}

type serverClientSession struct {
	assignedIP netip.Addr
	session    *Session
	cancel     context.CancelFunc
}

// Server represents an RFC 9484 (Proxying IP in HTTP) and RFC 9298 compliant MASQUE tunneling server (RFC 9484 §4.1).
type Server struct {
	ipam     *IPAM
	opts     ServerConfig
	mu       sync.RWMutex
	sessions map[netip.Addr]*serverClientSession
	closed   atomic.Bool
	done     chan struct{}
	conns    sync.WaitGroup
}

// NewServer constructs a new MASQUE proxying [Server] with IPAM and packet routing per RFC 9484 Section 4.1.
func NewServer(cfg ServerConfig) *Server {
	if cfg.MaxMTU <= 0 {
		cfg.MaxMTU = 1500
	}

	return &Server{
		ipam:     NewIPAM(cfg.IPv4Prefix, cfg.IPv6Prefix),
		opts:     cfg,
		sessions: make(map[netip.Addr]*serverClientSession),
		done:     make(chan struct{}),
	}
}

// HandleSession negotiates client IP assignment via Capsule protocol (RFC 9484 §4.7.1) and drives packet routing (RFC 9484 §7.2).
func (srv *Server) HandleSession(ctx context.Context, session *Session) error {
	if srv.closed.Load() {
		return ErrServerClosed
	}

	srv.conns.Add(1)
	defer srv.conns.Done()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Allocate client IP (RFC 9484 §4.7.1)
	assigned, err := srv.ipam.Allocate(nil)
	if err != nil {
		return err
	}

	defer srv.ipam.Release(assigned.Addr)

	clientSess := &serverClientSession{
		assignedIP: assigned.Addr,
		session:    session,
		cancel:     cancel,
	}

	srv.mu.Lock()
	srv.sessions[assigned.Addr] = clientSess
	srv.mu.Unlock()

	defer func() {
		srv.mu.Lock()
		delete(srv.sessions, assigned.Addr)
		srv.mu.Unlock()
	}()

	// Send ADDRESS_ASSIGN capsule
	var assignBuf [256]byte

	n := EncodeAddressAssignPayload([]AssignedAddress{assigned}, assignBuf[:])
	if writeErr := session.WriteCapsule(CapsuleAddressAssign, assignBuf[:n]); writeErr != nil {
		return writeErr
	}

	// Asynchronous capsule reader loop
	go func() {
		for {
			cType, _, cErr := session.ReadCapsule()
			if cErr != nil {
				return
			}

			_ = cType
		}
	}()

	replyFunc := func(replyPkt []byte) error {
		return srv.RoutePacket(replyPkt)
	}

	// Datagram processing loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-srv.done:
			return ErrServerClosed
		default:
			pkt, rErr := session.ReceiveIPPacket(ctx)
			if rErr != nil {
				if errors.Is(rErr, context.Canceled) || errors.Is(rErr, netErrClosed) {
					return nil
				}

				return rErr
			}

			if len(pkt) == 0 {
				continue
			}

			// Validate source IP to prevent IP spoofing
			srcIP := ExtractSrcIP(pkt)
			if srcIP.IsValid() && srcIP != assigned.Addr {
				// Drop spoofed packet
				continue
			}

			if srv.opts.PacketHandler != nil {
				if hErr := srv.opts.PacketHandler(pkt, assigned.Addr, replyFunc); hErr != nil {
					continue
				}
			} else {
				_ = srv.RoutePacket(pkt)
			}
		}
	}
}

// RoutePacket inspects destination IP of packet and delivers it to matching client session per RFC 9484 Section 7.2 (Routing Operation).
func (srv *Server) RoutePacket(packet []byte) error {
	destIP := ExtractDestIP(packet)
	if !destIP.IsValid() {
		return ErrInvalidIPHeader
	}

	srv.mu.RLock()
	clientSess, ok := srv.sessions[destIP]
	srv.mu.RUnlock()

	if !ok || clientSess == nil {
		return ErrNoRouteToHost
	}

	return clientSess.session.SendIPPacket(packet)
}

// ActiveSessions returns the number of currently connected client sessions.
func (srv *Server) ActiveSessions() int {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	return len(srv.sessions)
}

// Close gracefully closes the server and terminates active client sessions.
func (srv *Server) Close() error {
	if srv.closed.CompareAndSwap(false, true) {
		close(srv.done)

		srv.mu.Lock()
		for _, sess := range srv.sessions {
			sess.cancel()
			_ = sess.session.Close()
		}

		srv.mu.Unlock()
	}

	return nil
}

// Shutdown stops the server and waits for all active sessions to finish or context cancellation.
func (srv *Server) Shutdown(ctx context.Context) error {
	_ = srv.Close()

	c := make(chan struct{})
	go func() {
		srv.conns.Wait()
		close(c)
	}()

	select {
	case <-c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
