// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socketio

import (
	"context"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni/realtime/ws"
)

// BackoffStrategy defines the contract for calculating dynamic reconnect backoff delays.
type BackoffStrategy interface {
	Next() time.Duration
	Reset()
}

func newBackoff(cfg Config) *generic.Backoff {
	return generic.NewBackoff(cfg.ReconnectionDelay, cfg.ReconnectionDelayMax, 2, cfg.JitterFactor)
}

func (s *Conn) reconnectLoop() {
	for {
		select {
		case <-s.getClosedChan():
			return
		default:
		}

		delay := s.backoff.Next()
		if !s.sleepBeforeReconnect(delay) {
			return
		}

		s.mu.Lock()
		s.attemptCount++
		attempt := s.attemptCount
		cb := s.onReconnecting
		s.mu.Unlock()

		if s.attemptReconnection(attempt, cb) {
			return
		}
	}
}

func (s *Conn) sleepBeforeReconnect(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	select {
	case <-s.getClosedChan():
		timer.Stop()
		return false
	case <-timer.C:
		return true
	}
}

func (s *Conn) attemptReconnection(attempt int, onReconnecting func(int)) bool {
	if onReconnecting != nil {
		go onReconnecting(attempt)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.ConnectTimeout)
	defer cancel()

	conn, resp, err := ws.DialWebSocket(ctx, s.client, s.targetURL, s.mods...)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		return s.handleReconnectFailure()
	}

	s.conn.Store(&conn)
	s.resetConnectionState()

	if err := s.doHandshake(context.Background()); err != nil {
		_ = conn.Close()
		return s.handleReconnectFailure()
	}

	s.finalizeReconnection()

	return true
}

func (s *Conn) handleReconnectFailure() bool {
	s.mu.RLock()
	attempt := s.attemptCount
	failCb := s.onReconnectFailed
	s.mu.RUnlock()

	if s.config.ReconnectionAttempts > 0 && attempt >= s.config.ReconnectionAttempts {
		if failCb != nil {
			go failCb()
		}

		return true
	}

	return false
}

func (s *Conn) resetConnectionState() {
	s.mu.Lock()
	s.closed = make(chan struct{})
	s.skipReconnect = false
	s.mu.Unlock()
	s.binaryBuf = nil
}

func (s *Conn) finalizeReconnection() {
	_ = s.fsm.Transition(context.Background(), sioEventTypeReconnect)

	s.stateMu.Lock()
	s.state = sioStateOpen
	s.stateMu.Unlock()

	s.backoff.Reset()

	s.mu.Lock()
	s.attemptCount = 0
	reconnectedCb := s.onReconnected
	s.mu.Unlock()

	if reconnectedCb != nil {
		go reconnectedCb()
	}

	go s.readLoop()
	go s.heartbeatLoop()
}
