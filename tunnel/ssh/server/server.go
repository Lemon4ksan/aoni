// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package server provides a customizable, high-performance SSH server implementation.
package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

// Handler handles active SSH session execution.
type Handler func(Session)

// SubsystemHandler handles specialized SSH subsystems (e.g., SFTP).
type SubsystemHandler func(Session)

// PasswordHandler authenticates users via password.
type PasswordHandler func(ctx Context, password string) bool

// PublicKeyHandler authenticates users via public key.
type PublicKeyHandler func(ctx Context, key ssh.PublicKey) bool

// Server represents a high-performance SSH server with session handling and channel routing.
type Server struct {
	Addr                 string
	Handler              Handler
	HostSigners          []ssh.Signer
	UserCAKeys           []ssh.PublicKey
	PasswordHandler      PasswordHandler
	PublicKeyHandler     PublicKeyHandler
	SubsystemHandlers    map[string]SubsystemHandler
	GlobalRequestHandler GlobalRequestHandler
	Version              string

	listener net.Listener
	mu       sync.RWMutex
	closed   atomic.Bool
	done     chan struct{}
	conns    sync.WaitGroup
}

// New constructs an SSH server configured with optional Option settings.
func New(addr string, handler Handler, opts ...Option) (*Server, error) {
	srv := &Server{
		Addr:              addr,
		Handler:           handler,
		SubsystemHandlers: make(map[string]SubsystemHandler),
		done:              make(chan struct{}),
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(srv); err != nil {
			return nil, err
		}
	}

	return srv, nil
}

// AddHostKey adds a host key signer to the server configuration.
func (s *Server) AddHostKey(signer ssh.Signer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.HostSigners = append(s.HostSigners, signer)
}

// SetSubsystem registers a handler for a named subsystem (e.g., "sftp").
func (s *Server) SetSubsystem(name string, handler SubsystemHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.SubsystemHandlers == nil {
		s.SubsystemHandlers = make(map[string]SubsystemHandler)
	}

	s.SubsystemHandlers[name] = handler
}

// ListenAndServe starts listening on s.Addr and serves incoming SSH connections.
func (s *Server) ListenAndServe() error {
	addr := s.Addr
	if addr == "" {
		addr = ":22"
	}

	l, err := net.Listen("tcp", addr) //nolint:noctx
	if err != nil {
		return err
	}

	return s.Serve(l)
}

// Serve accepts and processes incoming SSH connections on l.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()

		_ = l.Close()

		return ErrServerClosed
	}

	s.listener = l
	s.mu.Unlock()

	config := s.buildServerConfig()

	for {
		conn, err := l.Accept()
		if err != nil {
			if s.closed.Load() {
				return ErrServerClosed
			}

			return err
		}

		s.conns.Add(1)

		go func(c net.Conn) {
			defer s.conns.Done()

			s.handleConn(c, config)
		}(conn)
	}
}

func (s *Server) buildServerConfig() *ssh.ServerConfig {
	version := s.Version
	if version == "" {
		version = "SSH-2.0-AoniServer"
	}

	config := &ssh.ServerConfig{
		ServerVersion: version,
	}

	for _, signer := range s.HostSigners {
		config.AddHostKey(signer)
	}

	if s.PasswordHandler != nil {
		config.PasswordCallback = func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			sessionID := hex.EncodeToString(conn.SessionID())

			ctx := newContext(context.Background(), conn.User(), conn.RemoteAddr(), conn.LocalAddr(), sessionID)
			if s.PasswordHandler(ctx, string(password)) {
				return nil, nil
			}

			return nil, ErrInvalidPassword
		}
	}

	if s.PublicKeyHandler != nil || len(s.UserCAKeys) > 0 {
		s.setupPublicKeyCallback(config)
	}

	return config
}

func (s *Server) setupPublicKeyCallback(config *ssh.ServerConfig) {
	certChecker := &ssh.CertChecker{
		IsUserAuthority: func(auth ssh.PublicKey) bool {
			for _, caKey := range s.UserCAKeys {
				if bytes.Equal(auth.Marshal(), caKey.Marshal()) {
					return true
				}
			}

			return false
		},
	}

	config.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		sessionID := hex.EncodeToString(conn.SessionID())
		ctx := newContext(context.Background(), conn.User(), conn.RemoteAddr(), conn.LocalAddr(), sessionID)

		if cert, ok := key.(*ssh.Certificate); ok {
			permissions, err := certChecker.Authenticate(conn, cert)
			if err != nil {
				return nil, err
			}

			if s.PublicKeyHandler != nil && !s.PublicKeyHandler(ctx, cert.Key) {
				return nil, ErrInvalidPublicKey
			}

			return permissions, nil
		}

		if s.PublicKeyHandler != nil && s.PublicKeyHandler(ctx, key) {
			return nil, nil
		}

		return nil, ErrInvalidPublicKey
	}
}

func (s *Server) handleConn(c net.Conn, config *ssh.ServerConfig) {
	sConn, chans, reqs, err := ssh.NewServerConn(c, config)
	if err != nil {
		_ = c.Close()
		return
	}
	defer sConn.Close()

	sessionID := hex.EncodeToString(sConn.SessionID())
	ctx := newContext(context.Background(), sConn.User(), sConn.RemoteAddr(), sConn.LocalAddr(), sessionID)

	if s.GlobalRequestHandler != nil {
		go s.GlobalRequestHandler(ctx, reqs, sConn)
	} else {
		go ssh.DiscardRequests(reqs)
	}

	for newChan := range chans {
		s.handleNewChannel(ctx, newChan)
	}
}

func (s *Server) handleNewChannel(ctx Context, newChan ssh.NewChannel) {
	switch newChan.ChannelType() {
	case "session":
		ch, requests, err := newChan.Accept()
		if err != nil {
			return
		}

		go s.handleSessionChannel(ctx, ch, requests)

	case "direct-tcpip":
		ch, requests, err := newChan.Accept()
		if err != nil {
			return
		}

		go s.handleDirectTcpipChannel(ch, requests, newChan.ExtraData())

	default:
		_ = newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
	}
}

func (s *Server) handleSessionChannel(ctx Context, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()

	sess := newSession(ch, ctx)

	for req := range reqs {
		if s.dispatchSessionRequest(sess, req) {
			return
		}
	}
}

func (s *Server) dispatchSessionRequest(sess *session, req *ssh.Request) (finished bool) {
	switch req.Type {
	case "pty-req":
		if pty, ok := parsePtyReq(req.Payload); ok {
			sess.mu.Lock()
			sess.pty = pty
			sess.hasPty = true
			sess.mu.Unlock()

			_ = req.Reply(true, nil)
		} else {
			_ = req.Reply(false, nil)
		}

		return false

	case "window-change":
		if w, h, ok := parsePtyDims(req.Payload); ok {
			sess.updatePtyDims(w, h)

			_ = req.Reply(true, nil)
		} else {
			_ = req.Reply(false, nil)
		}

		return false

	case "env":
		var envVar struct{ Key, Value string }
		if err := ssh.Unmarshal(req.Payload, &envVar); err == nil {
			sess.mu.Lock()
			sess.env = append(sess.env, envVar.Key+"="+envVar.Value)
			sess.mu.Unlock()
		}

		_ = req.Reply(true, nil)

		return false

	case "exec":
		var msg struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &msg); err == nil {
			sess.rawCmd = msg.Command
			sess.cmd = strings.Fields(msg.Command)
		}

		_ = req.Reply(true, nil)

		if s.Handler != nil {
			s.Handler(sess)
		}

		return true

	case "subsystem":
		var msg struct{ Name string }
		if err := ssh.Unmarshal(req.Payload, &msg); err == nil {
			sess.subsystem = msg.Name
		}

		_ = req.Reply(true, nil)

		s.mu.RLock()
		subHandler := s.SubsystemHandlers[sess.subsystem]
		s.mu.RUnlock()

		if subHandler != nil {
			subHandler(sess)
		}

		return true

	default:
		_ = req.Reply(false, nil)
		return false
	}
}

func (s *Server) handleDirectTcpipChannel(ch ssh.Channel, reqs <-chan *ssh.Request, extra []byte) {
	defer ch.Close()

	go ssh.DiscardRequests(reqs)

	type directMsg struct {
		RAddr string
		RPort uint32
		LAddr string
		LPort uint32
	}

	var msg directMsg
	if err := ssh.Unmarshal(extra, &msg); err != nil {
		return
	}

	dest := net.JoinHostPort(msg.RAddr, strconv.FormatUint(uint64(msg.RPort), 10))

	destConn, err := net.Dial("tcp", dest) //nolint:noctx
	if err != nil {
		return
	}
	defer destConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() { defer wg.Done(); _, _ = io.Copy(ch, destConn) }()
	go func() { defer wg.Done(); _, _ = io.Copy(destConn, ch) }()

	wg.Wait()
}

func parsePtyReq(payload []byte) (pty Pty, ok bool) {
	if len(payload) < 4 {
		return Pty{}, false
	}

	termLen := int(binary.BigEndian.Uint32(payload[0:4]))
	if len(payload) < 4+termLen+16 {
		return Pty{}, false
	}

	term := string(payload[4 : 4+termLen])
	rest := payload[4+termLen:]

	width := int(binary.BigEndian.Uint32(rest[0:4]))
	height := int(binary.BigEndian.Uint32(rest[4:8]))

	return Pty{
		Term:   term,
		Width:  width,
		Height: height,
	}, true
}

func parsePtyDims(payload []byte) (width, height int, ok bool) {
	if len(payload) < 8 {
		return 80, 40, false
	}

	w := int(binary.BigEndian.Uint32(payload[0:4]))
	h := int(binary.BigEndian.Uint32(payload[4:8]))

	return w, h, true
}

// Close immediately closes the listener and terminates active connections.
func (s *Server) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.mu.Lock()
		if s.listener != nil {
			_ = s.listener.Close()
		}

		s.mu.Unlock()

		close(s.done)
	}

	return nil
}

// Shutdown gracefully shuts down the SSH server waiting for connections to finish or ctx cancellation.
func (s *Server) Shutdown(ctx context.Context) error {
	_ = s.Close()

	c := make(chan struct{})
	go func() {
		s.conns.Wait()
		close(c)
	}()

	select {
	case <-c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
