// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// SocketIOConn provides basic support for working with Socket.IO v4 / Engine.IO v4 servers.
// Supports sending and reading events, as well as background heartbeat packet exchange.
type SocketIOConn struct {
	conn    net.Conn
	sid     string
	onEvent map[string]func(args []json.RawMessage)
	onClose func()
	writeMu sync.Mutex
	mu      sync.RWMutex
	closed  chan struct{}
}

// DialSocketIO connects to the Socket.IO v4 server via the aoni WebSocket connection,
// performs a handshake and starts background workers to maintain activity.
func DialSocketIO(
	ctx context.Context,
	c *Client,
	targetURL string,
	mods ...RequestModifier,
) (*SocketIOConn, error) {
	conn, _, err := DialWebSocket(ctx, c, targetURL, mods...)
	if err != nil {
		return nil, err
	}

	pType, payload, err := readEIOPacket(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni sio: handshake failed: %w", err)
	}
	if pType != '0' {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni sio: expected EIO open packet, got %c", pType)
	}

	var params struct {
		SID          string   `json:"sid"`
		Upgrades     []string `json:"upgrades"`
		PingInterval int      `json:"pingInterval"`
		PingTimeout  int      `json:"pingTimeout"`
	}
	if err := json.Unmarshal(payload, &params); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni sio: unmarshal open params failed: %w", err)
	}

	if err := writeEIOPacket(conn, '4', []byte("0")); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni sio: failed to send connect: %w", err)
	}

	pType, payload, err = readEIOPacket(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni sio: read connect response failed: %w", err)
	}
	if pType != '4' || len(payload) < 1 || payload[0] != '0' {
		_ = conn.Close()
		return nil, fmt.Errorf("aoni sio: unexpected connect response: %c%s", pType, string(payload))
	}

	sio := &SocketIOConn{
		conn:    conn,
		sid:     params.SID,
		onEvent: make(map[string]func(args []json.RawMessage)),
		closed:  make(chan struct{}),
	}

	go sio.readLoop()
	go sio.heartbeatLoop(time.Duration(params.PingInterval) * time.Millisecond)

	return sio, nil
}

// On registers a handler for a specific Socket.IO event.
func (s *SocketIOConn) On(event string, handler func(args []json.RawMessage)) {
	s.mu.Lock()
	s.onEvent[event] = handler
	s.mu.Unlock()
}

// OnClose sets the callback function to be called when the connection is closed.
func (s *SocketIOConn) OnClose(handler func()) {
	s.mu.Lock()
	s.onClose = handler
	s.mu.Unlock()
}

// Emit sends a Socket.IO event with arbitrary arguments.
func (s *SocketIOConn) Emit(event string, args ...any) error {
	payload := make([]any, 1+len(args))
	payload[0] = event
	for i, arg := range args {
		payload[i+1] = arg
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	sioPayload := make([]byte, 1+len(data))
	sioPayload[0] = '2'
	copy(sioPayload[1:], data)

	return s.writePacket('4', sioPayload)
}

// Close correctly closes the connection and sends an Engine.IO close packet.
func (s *SocketIOConn) Close() error {
	s.mu.Lock()
	select {
	case <-s.closed:
		s.mu.Unlock()
		return nil
	default:
		close(s.closed)
	}
	s.mu.Unlock()

	_ = s.writePacket('1', nil)
	return s.conn.Close()
}

func (s *SocketIOConn) writePacket(pType byte, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeEIOPacket(s.conn, pType, payload)
}

func (s *SocketIOConn) heartbeatLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			if err := s.writePacket('2', nil); err != nil {
				_ = s.Close()
				return
			}
		}
	}
}

func (s *SocketIOConn) readLoop() {
	defer func() {
		_ = s.Close()
		s.mu.RLock()
		cb := s.onClose
		s.mu.RUnlock()
		if cb != nil {
			cb()
		}
	}()

	for {
		pType, payload, err := readEIOPacket(s.conn)
		if err != nil {
			return
		}

		switch pType {
		case '1': // Engine.IO CLOSE
			return
		case '3': // Engine.IO PONG (response from server to our PING)
			// Heartbeat confirmed
		case '4': // Engine.IO MESSAGE
			if len(payload) == 0 {
				continue
			}
			sioType := payload[0]
			sioData := payload[1:]

			switch sioType {
			case '1': // Socket.IO DISCONNECT
				return
			case '2': // Socket.IO EVENT
				s.handleEvent(sioData)
			}
		}
	}
}

func (s *SocketIOConn) handleEvent(data []byte) {
	// Skip namespace prefix
	if len(data) > 0 && data[0] == '/' {
		commaIdx := bytes.IndexByte(data, ',')
		if commaIdx != -1 {
			data = data[commaIdx+1:]
		}
	}

	var rawArgs []json.RawMessage
	if err := json.Unmarshal(data, &rawArgs); err != nil {
		return
	}

	if len(rawArgs) == 0 {
		return
	}

	var eventName string
	if err := json.Unmarshal(rawArgs[0], &eventName); err != nil {
		return
	}

	s.mu.RLock()
	handler, ok := s.onEvent[eventName]
	s.mu.RUnlock()

	if ok && handler != nil {
		go handler(rawArgs[1:])
	}
}

func readEIOPacket(conn net.Conn) (byte, []byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 1024)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, nil, err
		}
	}
	data := buf.Bytes()
	if len(data) == 0 {
		return 0, nil, io.EOF
	}
	return data[0], data[1:], nil
}

func writeEIOPacket(conn net.Conn, pType byte, payload []byte) error {
	data := make([]byte, 1+len(payload))
	data[0] = pType
	copy(data[1:], payload)

	_, err := conn.Write(data)
	return err
}
