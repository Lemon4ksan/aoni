// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socketio

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/lemon4ksan/foundation/async/fsm"

	"github.com/lemon4ksan/aoni/realtime/ws"
)

type sioConnState int

const (
	sioStateClosed sioConnState = iota
	sioStateOpening
	sioStateOpen
	sioStateClosing
)

type sioEventType int

const (
	sioEventTypeOpen sioEventType = iota
	sioEventTypeClose
	sioEventTypeReconnect
)

func initFSM() *fsm.FSM[sioConnState, sioEventType] {
	sm := fsm.NewFSM[sioConnState, sioEventType](sioStateClosed)
	sm.AddRules(
		fsm.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateClosed,
			Event: sioEventTypeOpen,
			To:    sioStateOpen,
		},
		fsm.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateOpen,
			Event: sioEventTypeClose,
			To:    sioStateClosing,
		},
		fsm.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateClosed,
			Event: sioEventTypeReconnect,
			To:    sioStateOpen,
		},
		fsm.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateOpening,
			Event: sioEventTypeOpen,
			To:    sioStateOpen,
		},
		fsm.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateOpening,
			Event: sioEventTypeClose,
			To:    sioStateClosed,
		},
		fsm.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateClosing,
			Event: sioEventTypeClose,
			To:    sioStateClosed,
		},
	)

	return sm
}

func (s *Conn) doHandshake(ctx context.Context) error {
	c := s.conn.Load()
	if c == nil {
		return ErrNotConnected
	}

	if err := s.readAndParseEIOOpen(ctx, *c); err != nil {
		return err
	}

	if err := s.sendConnect(); err != nil {
		return fmt.Errorf("aoni/socketio: send connect: %w", err)
	}

	return s.readAndParseSIOConnect(ctx, *c)
}

func (s *Conn) readAndParseEIOOpen(ctx context.Context, conn ws.Conn) error {
	pType, payload, err := readEIOPacketCtx(ctx, conn)
	if err != nil {
		return fmt.Errorf("aoni/socketio: handshake failed: %w", err)
	}

	if pType != eioOpen {
		return fmt.Errorf("aoni/socketio: expected EIO open packet, got %c", pType)
	}

	var params struct {
		SID          string `json:"sid"`
		PingInterval int    `json:"pingInterval"`
		PingTimeout  int    `json:"pingTimeout"`
	}

	if err := json.Unmarshal(payload, &params); err != nil {
		return fmt.Errorf("aoni/socketio: unmarshal open params: %w", err)
	}

	s.mu.Lock()
	s.sid = params.SID
	s.pingInterval = time.Duration(params.PingInterval) * time.Millisecond
	s.mu.Unlock()

	return nil
}

func (s *Conn) readAndParseSIOConnect(ctx context.Context, conn ws.Conn) error {
	pType, payload, err := readEIOPacketCtx(ctx, conn)
	if err != nil {
		return fmt.Errorf("aoni/socketio: read connect response: %w", err)
	}

	if pType != eioMessage || len(payload) < 1 || payload[0] != sioConnect {
		if pType == eioMessage && len(payload) > 0 && payload[0] == sioConnectError {
			return fmt.Errorf("aoni/socketio: connect rejected: %s", string(payload[1:]))
		}

		return fmt.Errorf("aoni/socketio: unexpected connect response: %c%s", pType, string(payload))
	}

	var connectResp struct {
		SID string `json:"sid"`
		PID string `json:"pid"`
	}

	_ = json.Unmarshal(payload[1:], &connectResp)

	if connectResp.PID != "" {
		s.mu.Lock()
		s.pid = connectResp.PID
		s.mu.Unlock()
	}

	return nil
}

func (s *Conn) sendConnect() error {
	var data json.RawMessage

	authData := make(map[string]any)
	if s.config.Auth != nil {
		switch v := s.config.Auth.(type) {
		case map[string]any:
			maps.Copy(authData, v)
		default:
			b, err := json.Marshal(s.config.Auth)
			if err != nil {
				return fmt.Errorf("aoni/socketio: marshal auth: %w", err)
			}

			authData["token"] = json.RawMessage(b)
		}
	}

	s.mu.RLock()
	pid := s.pid
	offset := s.offset
	s.mu.RUnlock()

	if pid != "" {
		authData["pid"] = pid
	}

	if offset != "" {
		authData["offset"] = offset
	}

	if len(authData) > 0 {
		var err error

		data, err = json.Marshal(authData)
		if err != nil {
			return fmt.Errorf("aoni/socketio: marshal auth: %w", err)
		}
	}

	payload := encodeSIOPacket(sioPacket{
		Type:      sioConnect,
		Namespace: s.namespace,
		Data:      data,
	})

	return s.writeEIOPacket(eioMessage, payload)
}

func (s *Conn) readEIOPacket() (byte, []byte, error) {
	c := s.conn.Load()
	if c == nil {
		return 0, nil, ErrNotConnected
	}

	return readSingleEIOPacket(*c)
}

func readEIOPacketCtx(ctx context.Context, conn ws.Conn) (byte, []byte, error) {
	type result struct {
		pType   byte
		payload []byte
		err     error
	}

	ch := make(chan result, 1)
	go func() {
		pType, payload, err := readSingleEIOPacket(conn)
		if err != nil {
			ch <- result{err: err}
			return
		}

		ch <- result{pType: pType, payload: payload}
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close()
		return 0, nil, ctx.Err()
	case r := <-ch:
		return r.pType, r.payload, r.err
	}
}

func (s *Conn) writeEIOPacket(pType byte, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	c := s.conn.Load()
	if c == nil {
		return ErrNotConnected
	}

	if pType == eioBinary {
		return (*c).WriteMessage(ws.FrameBinary, payload)
	}

	data := make([]byte, 1+len(payload))
	data[0] = pType
	copy(data[1:], payload)

	return (*c).WriteMessage(ws.FrameText, data)
}

func (s *Conn) readLoop() {
	defer s.cleanupConnection()

	for {
		pType, payload, err := s.readEIOPacket()
		if err != nil {
			return
		}

		if s.dispatchEIOPacket(pType, payload) {
			return
		}
	}
}

func (s *Conn) cleanupConnection() {
	_ = s.fsm.Transition(context.Background(), sioEventTypeClose)
	_ = s.fsm.Transition(context.Background(), sioEventTypeClose)

	s.stateMu.Lock()
	s.state = sioStateClosed
	s.stateMu.Unlock()

	if c := s.conn.Load(); c != nil {
		_ = (*c).Close()
	}

	s.mu.RLock()
	cb := s.onClose
	skipReconnect := s.skipReconnect
	s.mu.RUnlock()

	if cb != nil {
		go cb()
	}

	if !skipReconnect && s.config.Reconnection {
		go s.reconnectLoop()
	}
}

func (s *Conn) dispatchEIOPacket(pType byte, payload []byte) bool {
	switch pType {
	case eioClose:
		return true

	case eioPong:
		s.pongMu.Lock()
		ch := s.pongCh
		s.pongMu.Unlock()

		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}

	case eioMessage:
		if len(payload) > 0 {
			s.handleSIOPacket(payload)
		}

	case eioBinary:
		if s.binaryBuf != nil && s.binaryBuf.addBuffer(payload) {
			pkt, err := s.binaryBuf.reconstruct()
			s.binaryBuf = nil

			if err == nil {
				s.dispatchPacket(pkt)
			}
		}
	}

	return false
}

func (s *Conn) handleSIOPacket(data []byte) {
	pkt, err := decodeSIOPacket(data)
	if err != nil {
		return
	}

	switch pkt.Type {
	case sioConnect:
		s.handleSIOConnect(pkt)
	case sioEvent:
		s.dispatchPacket(pkt)
	case sioAck:
		if pkt.ID != nil {
			s.handleAck(*pkt.ID, pkt.Data)
		}
	case sioBinaryEvent, sioBinaryAck:
		s.binaryBuf = newBinaryReconstructor(pkt.Attachments, pkt)
	case sioConnectError:
		s.handleSIOConnectError()
	case sioDisconnect:
		return
	}
}

func (s *Conn) handleSIOConnect(pkt *sioPacket) {
	var connectResp struct {
		SID string `json:"sid"`
		PID string `json:"pid"`
	}

	if pkt.Data != nil {
		_ = json.Unmarshal(pkt.Data, &connectResp)
	}

	s.mu.Lock()
	if connectResp.SID != "" {
		s.sid = connectResp.SID
	}

	if connectResp.PID != "" {
		s.pid = connectResp.PID
	}

	s.mu.Unlock()
}

func (s *Conn) handleSIOConnectError() {
	s.mu.RLock()
	cb := s.onClose
	s.mu.RUnlock()

	if cb != nil {
		go cb()
	}
}

func (s *Conn) dispatchPacket(pkt *sioPacket) {
	var rawArgs []json.RawMessage
	if err := json.Unmarshal(pkt.Data, &rawArgs); err != nil || len(rawArgs) == 0 {
		return
	}

	var eventName string
	if err := json.Unmarshal(rawArgs[0], &eventName); err != nil {
		return
	}

	nsp := pkt.Namespace
	if nsp == "" {
		nsp = s.namespace
	}

	s.mu.RLock()

	handlers := s.nsEvents[nsp]
	if handlers != nil {
		if handler, ok := handlers[eventName]; ok && handler != nil {
			go handler(rawArgs[1:])
		}

		if catchAll, ok := handlers["*"]; ok && catchAll != nil {
			go catchAll(rawArgs)
		}
	}

	s.mu.RUnlock()
}

func (s *Conn) handleAck(id int64, data json.RawMessage) {
	var args []json.RawMessage
	if err := json.Unmarshal(data, &args); err != nil {
		return
	}

	s.ackMgr.Resolve(id, args, nil)
}

func (s *Conn) heartbeatLoop() {
	s.mu.RLock()
	interval := s.pingInterval
	s.mu.RUnlock()

	if interval <= 0 {
		interval = 25 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.getClosedChan():
			return
		case <-ticker.C:
			s.pongMu.Lock()
			s.pongCh = make(chan struct{}, 1)
			ch := s.pongCh
			s.pongMu.Unlock()

			if err := s.writeEIOPacket(eioPing, nil); err != nil {
				return
			}

			pingTimer := time.NewTimer(s.config.PingTimeout)
			select {
			case <-ch:
				pingTimer.Stop()
			case <-pingTimer.C:
				s.mu.Lock()
				if !s.skipReconnect {
					select {
					case <-s.closed:
					default:
						close(s.closed)
					}
				}

				s.mu.Unlock()

				return

			case <-s.getClosedChan():
				pingTimer.Stop()
				return
			}
		}
	}
}
