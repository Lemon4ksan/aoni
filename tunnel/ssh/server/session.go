// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Session represents an active interactive or exec SSH session channel.
//
// Provides methods to query environment variables, command arguments,
// terminal dimensions, and subscribe to real-time window resize events (window-change).
type Session interface {
	io.ReadWriter
	ssh.Channel

	Context() Context
	User() string
	RemoteAddr() net.Addr
	LocalAddr() net.Addr
	Command() []string
	RawCommand() string
	Environ() []string
	Pty() (Pty, bool)
	WindowChanges() <-chan Pty
	Subsystem() string
	Exit(code int) error
	Stderr() io.ReadWriter
}

type session struct {
	ssh.Channel

	ctx           Context
	rawCmd        string
	cmd           []string
	env           []string
	pty           Pty
	windowChanges chan Pty
	hasPty        bool
	subsystem     string
	mu            sync.RWMutex
}

func newSession(ch ssh.Channel, ctx Context) *session {
	return &session{
		Channel:       ch,
		ctx:           ctx,
		windowChanges: make(chan Pty, 1),
	}
}

func (s *session) Context() Context {
	return s.ctx
}

func (s *session) User() string {
	return s.ctx.User()
}

func (s *session) RemoteAddr() net.Addr {
	return s.ctx.RemoteAddr()
}

func (s *session) LocalAddr() net.Addr {
	return s.ctx.LocalAddr()
}

func (s *session) Command() []string {
	return s.cmd
}

func (s *session) RawCommand() string {
	return s.rawCmd
}

func (s *session) Environ() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.env
}

func (s *session) Pty() (Pty, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.pty, s.hasPty
}

func (s *session) WindowChanges() <-chan Pty {
	return s.windowChanges
}

func (s *session) Subsystem() string {
	return s.subsystem
}

func (s *session) Exit(code int) error {
	type exitStatus struct {
		Status uint32
	}

	_, err := s.SendRequest("exit-status", false, ssh.Marshal(exitStatus{Status: uint32(code)}))

	return err
}

func (s *session) updatePtyDims(width, height int) {
	s.mu.Lock()
	s.pty.Width = width
	s.pty.Height = height
	currentPty := s.pty
	s.mu.Unlock()

	select {
	case s.windowChanges <- currentPty:
	default:
		select {
		case <-s.windowChanges:
		default:
		}

		select {
		case s.windowChanges <- currentPty:
		default:
		}
	}
}
