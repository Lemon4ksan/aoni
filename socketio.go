// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/rand/v2"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	eioOpen    = '0'
	eioClose   = '1'
	eioPing    = '2'
	eioPong    = '3'
	eioMessage = '4'
	eioUpgrade = '5'
	eioNoop    = '6'
	eioBinary  = 'b'
)

const (
	sioConnect      byte = '0'
	sioDisconnect   byte = '1'
	sioEvent        byte = '2'
	sioAck          byte = '3'
	sioConnectError byte = '4'
	sioBinaryEvent  byte = '5'
	sioBinaryAck    byte = '6'
)

type sioConnState int

const (
	sioStateClosed sioConnState = iota
	sioStateOpening
	sioStateOpen
	sioStateClosing
)

// SocketIOConfig holds all configurable parameters for a Socket.IO connection.
// Zero values mean "use default". Passing a zero-valued config is safe.
type SocketIOConfig struct {
	// Reconnection controls automatic reconnection on unexpected disconnect.
	Reconnection bool
	// ReconnectionAttempts is the maximum number of reconnection attempts.
	// 0 means unlimited.
	ReconnectionAttempts int
	// ReconnectionDelay is the initial delay before the first reconnection attempt.
	// Default: 1s.
	ReconnectionDelay time.Duration
	// ReconnectionDelayMax is the upper bound for reconnection delay.
	// Default: 30s.
	ReconnectionDelayMax time.Duration
	// JitterFactor controls random delay variation (0..1). Default: 0.5.
	JitterFactor float64

	// ConnectTimeout is the timeout for the full connection handshake (WS + EIO + SIO).
	// Default: 20s.
	ConnectTimeout time.Duration
	// PingTimeout is the maximum time to wait for a pong after sending a ping.
	// Default: 20s.
	PingTimeout time.Duration

	// Namespace is the Socket.IO namespace to connect to. Default: "/".
	Namespace string
	// Auth is the authentication payload sent in the SIO CONNECT packet.
	Auth any
}

// resolveDefaults fills zero-valued config fields with sensible defaults.
func (cfg *SocketIOConfig) resolveDefaults() {
	if cfg.ReconnectionDelay == 0 {
		cfg.ReconnectionDelay = time.Second
	}

	if cfg.ReconnectionDelayMax == 0 {
		cfg.ReconnectionDelayMax = 30 * time.Second
	}

	if cfg.JitterFactor == 0 {
		cfg.JitterFactor = 0.5
	}

	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 20 * time.Second
	}

	if cfg.PingTimeout == 0 {
		cfg.PingTimeout = 20 * time.Second
	}

	if cfg.Namespace == "" {
		cfg.Namespace = "/"
	}
}

// NamespaceSocket is a namespace-scoped event emitter.
// Obtain via [SocketIOConn.OnNamespace].
type NamespaceSocket struct {
	conn *SocketIOConn
	nsp  string
}

// On registers a handler for a specific event on this namespace.
func (ns *NamespaceSocket) On(event string, handler func(args []json.RawMessage)) {
	ns.conn.setNamespaceHandler(ns.nsp, event, handler)
}

// Emit sends a Socket.IO event on this namespace.
// If the last argument is a func(args []json.RawMessage), it is used as an ACK callback.
func (ns *NamespaceSocket) Emit(event string, args ...any) error {
	return ns.conn.emitNS(ns.nsp, event, args...)
}

// EmitWithAck sends an event and blocks until the server acknowledges it or ctx expires.
func (ns *NamespaceSocket) EmitWithAck(ctx context.Context, event string, args ...any) ([]json.RawMessage, error) {
	return ns.conn.emitWithAckNS(ctx, ns.nsp, event, args...)
}

// EmitVolatile sends an event only if currently connected; silently drops otherwise.
func (ns *NamespaceSocket) EmitVolatile(event string, args ...any) error {
	return ns.conn.emitVolatileNS(ns.nsp, event, args...)
}

// SocketIOConn provides support for working with Socket.IO v5 / Engine.IO v4 servers.
// Supports event-based communication, namespace multiplexing, acknowledgements,
// binary data, and automatic reconnection.
type SocketIOConn struct {
	conn net.Conn
	sid  string

	writeMu sync.Mutex
	mu      sync.RWMutex
	closed  chan struct{}

	// Configuration
	config    SocketIOConfig
	namespace string

	// Per-namespace event handlers: namespace -> event -> handler
	nsEvents map[string]map[string]func(args []json.RawMessage)
	onClose  func()

	// Reconnection callbacks
	onReconnecting    func(attempt int)
	onReconnected     func()
	onReconnectFailed func()

	// ACK system
	ackMu  sync.Mutex
	acks   map[int64]*ackEntry
	ackSeq int64

	// State
	state   sioConnState
	stateMu sync.RWMutex

	// Reconnection
	backoff       *backoff
	skipReconnect bool
	reconnectStop chan struct{}
	client        *Client
	targetURL     string
	mods          []RequestModifier
	pingInterval  time.Duration

	// Ping timeout
	pongCh chan struct{}

	// Binary reconstruction
	binaryBuf *binaryReconstructor

	// Connection state recovery
	pid    string
	offset string
}

// DialSocketIO connects to a Socket.IO v5 server via the aoni WebSocket pipeline,
// performs the Engine.IO v4 handshake and Socket.IO v5 CONNECT, and starts
// background workers for heartbeats and packet reading.
func DialSocketIO(
	ctx context.Context,
	c *Client,
	targetURL string,
	config SocketIOConfig,
	mods ...RequestModifier,
) (*SocketIOConn, error) {
	config.resolveDefaults()

	conn, _, err := DialWebSocket(ctx, c, targetURL, mods...)
	if err != nil {
		return nil, fmt.Errorf("aoni sio: dial websocket: %w", err)
	}

	sio := &SocketIOConn{
		conn:          conn,
		config:        config,
		namespace:     config.Namespace,
		nsEvents:      make(map[string]map[string]func(args []json.RawMessage)),
		acks:          make(map[int64]*ackEntry),
		closed:        make(chan struct{}),
		reconnectStop: make(chan struct{}),
		client:        c,
		targetURL:     targetURL,
		mods:          mods,
		backoff:       newBackoff(config),
	}

	if err := sio.doHandshake(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	sio.stateMu.Lock()
	sio.state = sioStateOpen
	sio.stateMu.Unlock()

	go sio.readLoop()
	go sio.heartbeatLoop()

	return sio, nil
}

// doHandshake performs the EIO open + SIO CONNECT handshake.
func (s *SocketIOConn) doHandshake(ctx context.Context) error {
	// Read EIO OPEN packet
	pType, payload, err := readEIOPacketCtx(ctx, s.conn)
	if err != nil {
		return fmt.Errorf("aoni sio: handshake failed: %w", err)
	}

	if pType != eioOpen {
		return fmt.Errorf("aoni sio: expected EIO open packet, got %c", pType)
	}

	var params struct {
		SID          string `json:"sid"`
		PingInterval int    `json:"pingInterval"`
		PingTimeout  int    `json:"pingTimeout"`
	}
	if err := json.Unmarshal(payload, &params); err != nil {
		return fmt.Errorf("aoni sio: unmarshal open params: %w", err)
	}

	s.sid = params.SID
	s.pingInterval = time.Duration(params.PingInterval) * time.Millisecond

	// Send SIO CONNECT with auth
	if err := s.sendConnect(); err != nil {
		return fmt.Errorf("aoni sio: send connect: %w", err)
	}

	// Read SIO CONNECT response
	pType, payload, err = readEIOPacketCtx(ctx, s.conn)
	if err != nil {
		return fmt.Errorf("aoni sio: read connect response: %w", err)
	}

	if pType != eioMessage || len(payload) < 1 || payload[0] != sioConnect {
		if pType == eioMessage && len(payload) > 0 && payload[0] == sioConnectError {
			return fmt.Errorf("aoni sio: connect rejected: %s", string(payload[1:]))
		}

		return fmt.Errorf("aoni sio: unexpected connect response: %c%s", pType, string(payload))
	}

	// Parse CONNECT response for sid/pid
	var connectResp struct {
		SID string `json:"sid"`
		PID string `json:"pid"`
	}

	_ = json.Unmarshal(payload[1:], &connectResp)
	if connectResp.PID != "" {
		s.pid = connectResp.PID
	}

	return nil
}

// sendConnect sends a Socket.IO CONNECT packet with optional auth.
func (s *SocketIOConn) sendConnect() error {
	var data json.RawMessage

	authData := make(map[string]any)
	if s.config.Auth != nil {
		switch v := s.config.Auth.(type) {
		case map[string]any:
			maps.Copy(authData, v)
		default:
			b, err := json.Marshal(s.config.Auth)
			if err != nil {
				return fmt.Errorf("aoni sio: marshal auth: %w", err)
			}

			authData["token"] = json.RawMessage(b)
		}
	}

	if s.pid != "" {
		authData["pid"] = s.pid
	}

	if s.offset != "" {
		authData["offset"] = s.offset
	}

	if len(authData) > 0 {
		var err error

		data, err = json.Marshal(authData)
		if err != nil {
			return fmt.Errorf("aoni sio: marshal auth: %w", err)
		}
	}

	payload := encodeSIOPacket(sioPacket{
		Type:      sioConnect,
		Namespace: s.namespace,
		Data:      data,
	})

	return s.writeEIOPacket(eioMessage, payload)
}

// On registers a handler for a specific Socket.IO event on the default namespace.
func (s *SocketIOConn) On(event string, handler func(args []json.RawMessage)) {
	s.setNamespaceHandler(s.namespace, event, handler)
}

// OnNamespace returns a [NamespaceSocket] scoped to the given namespace.
func (s *SocketIOConn) OnNamespace(nsp string) *NamespaceSocket {
	s.setNamespaceHandler(nsp, "", nil) // ensure map entry exists
	return &NamespaceSocket{conn: s, nsp: nsp}
}

// OnClose sets the callback invoked when the connection closes.
func (s *SocketIOConn) OnClose(handler func()) {
	s.mu.Lock()
	s.onClose = handler
	s.mu.Unlock()
}

// OnReconnecting sets the callback invoked before each reconnection attempt.
func (s *SocketIOConn) OnReconnecting(handler func(attempt int)) {
	s.mu.Lock()
	s.onReconnecting = handler
	s.mu.Unlock()
}

// OnReconnected sets the callback invoked after a successful reconnection.
func (s *SocketIOConn) OnReconnected(handler func()) {
	s.mu.Lock()
	s.onReconnected = handler
	s.mu.Unlock()
}

// OnReconnectFailed sets the callback invoked when all reconnection attempts are exhausted.
func (s *SocketIOConn) OnReconnectFailed(handler func()) {
	s.mu.Lock()
	s.onReconnectFailed = handler
	s.mu.Unlock()
}

// Emit sends a Socket.IO event on the default namespace.
// If the last argument is func(args []json.RawMessage), it is used as an ACK callback.
func (s *SocketIOConn) Emit(event string, args ...any) error {
	return s.emitNS(s.namespace, event, args...)
}

// EmitWithAck sends an event on the default namespace and blocks until the server
// acknowledges it or ctx expires.
func (s *SocketIOConn) EmitWithAck(ctx context.Context, event string, args ...any) ([]json.RawMessage, error) {
	return s.emitWithAckNS(ctx, s.namespace, event, args...)
}

// EmitVolatile sends an event only if currently connected; silently drops otherwise.
func (s *SocketIOConn) EmitVolatile(event string, args ...any) error {
	return s.emitVolatileNS(s.namespace, event, args...)
}

// Close sends a Socket.IO DISCONNECT and Engine.IO CLOSE, then shuts down the connection.
// Reconnection is suppressed.
func (s *SocketIOConn) Close() error {
	s.mu.Lock()
	select {
	case <-s.closed:
		s.mu.Unlock()
		return nil
	default:
		close(s.closed)
	}

	s.skipReconnect = true
	s.mu.Unlock()

	// Send SIO DISCONNECT on default namespace
	payload := encodeSIOPacket(sioPacket{
		Type:      sioDisconnect,
		Namespace: s.namespace,
	})
	_ = s.writeEIOPacket(eioMessage, payload)

	// Send EIO CLOSE.
	_ = s.writeEIOPacket(eioClose, nil)

	return s.conn.Close()
}

// SID returns the server-assigned session ID.
func (s *SocketIOConn) SID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sid
}

// Connected reports whether the connection is currently open.
func (s *SocketIOConn) Connected() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state == sioStateOpen
}

func (s *SocketIOConn) emitNS(nsp, event string, args ...any) error {
	s.stateMu.RLock()

	if s.state != sioStateOpen {
		s.stateMu.RUnlock()
		return errors.New("aoni sio: not connected")
	}

	s.stateMu.RUnlock()

	// Extract trailing ACK callback
	var ackFn func(args []json.RawMessage)

	emitArgs := args
	if len(args) > 0 {
		if fn, ok := args[len(args)-1].(func(args []json.RawMessage)); ok {
			ackFn = fn
			emitArgs = args[:len(args)-1]
		}
	}

	payload := make([]any, 1+len(emitArgs))
	payload[0] = event
	copy(payload[1:], emitArgs)

	// Check for binary data
	if hasBinary(payload) {
		return s.emitBinaryNS(nsp, payload, ackFn)
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("aoni sio: marshal event: %w", err)
	}

	pkt := sioPacket{
		Type:      sioEvent,
		Namespace: nsp,
		Data:      jsonData,
	}

	if ackFn != nil {
		s.ackMu.Lock()
		id := s.ackSeq
		s.ackSeq++
		pkt.ID = &id
		s.acks[id] = &ackEntry{fn: ackFn}
		s.ackMu.Unlock()
	}

	encoded := encodeSIOPacket(pkt)

	return s.writeEIOPacket(eioMessage, encoded)
}

func (s *SocketIOConn) emitBinaryNS(nsp string, data any, ackFn func(args []json.RawMessage)) error {
	deconstructed, buffers := deconstructBinary(data)

	jsonData, err := json.Marshal(deconstructed)
	if err != nil {
		return fmt.Errorf("aoni sio: marshal binary event: %w", err)
	}

	pkt := sioPacket{
		Type:        sioBinaryEvent,
		Namespace:   nsp,
		Attachments: len(buffers),
		Data:        jsonData,
	}

	if ackFn != nil {
		s.ackMu.Lock()
		id := s.ackSeq
		s.ackSeq++
		pkt.ID = &id
		s.acks[id] = &ackEntry{fn: ackFn}
		s.ackMu.Unlock()
	}

	encoded := encodeSIOPacket(pkt)
	if err := s.writeEIOPacket(eioMessage, encoded); err != nil {
		return err
	}

	for _, buf := range buffers {
		if err := s.writeEIOPacket(eioBinary, buf); err != nil {
			return fmt.Errorf("aoni sio: send binary attachment: %w", err)
		}
	}

	return nil
}

func (s *SocketIOConn) emitWithAckNS(ctx context.Context, nsp, event string, args ...any) ([]json.RawMessage, error) {
	ch := make(chan []json.RawMessage, 1)
	errCh := make(chan error, 1)

	callback := func(rawArgs []json.RawMessage) {
		select {
		case ch <- rawArgs:
		default:
		}
	}

	emitArgs := make([]any, len(args)+1)
	copy(emitArgs, args)
	emitArgs[len(args)] = callback

	if err := s.emitNS(nsp, event, emitArgs...); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		return result, nil
	case err := <-errCh:
		return nil, err
	}
}

func (s *SocketIOConn) emitVolatileNS(nsp, event string, args ...any) error {
	s.stateMu.RLock()
	connected := s.state == sioStateOpen
	s.stateMu.RUnlock()

	if !connected {
		return nil
	}

	return s.emitNS(nsp, event, args...)
}

func (s *SocketIOConn) setNamespaceHandler(nsp, event string, handler func(args []json.RawMessage)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.nsEvents[nsp] == nil {
		s.nsEvents[nsp] = make(map[string]func(args []json.RawMessage))
	}

	if event != "" {
		s.nsEvents[nsp][event] = handler
	}
}

func (s *SocketIOConn) readLoop() {
	defer func() {
		s.stateMu.Lock()
		s.state = sioStateClosed
		s.stateMu.Unlock()

		_ = s.conn.Close()

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
	}()

	for {
		pType, payload, err := s.readEIOPacket()
		if err != nil {
			return
		}

		switch pType {
		case eioClose:
			return
		case eioPong:
			// Signal pong received.
			select {
			case s.pongCh <- struct{}{}:
			default:
			}
		case eioMessage:
			if len(payload) == 0 {
				continue
			}

			s.handleSIOPacket(payload)
		case eioBinary:
			if s.binaryBuf != nil && s.binaryBuf.addBuffer(payload) {
				pkt, err := s.binaryBuf.reconstruct()

				s.binaryBuf = nil
				if err == nil {
					s.dispatchPacket(pkt)
				}
			}
		}
	}
}

func (s *SocketIOConn) handleSIOPacket(data []byte) {
	pkt, err := decodeSIOPacket(data)
	if err != nil {
		return
	}

	switch pkt.Type {
	case sioConnect:
		// Store sid from CONNECT response.
		var connectResp struct {
			SID string `json:"sid"`
			PID string `json:"pid"`
		}
		if pkt.Data != nil {
			_ = json.Unmarshal(pkt.Data, &connectResp)
		}

		if connectResp.SID != "" {
			s.mu.Lock()
			s.sid = connectResp.SID
			s.mu.Unlock()
		}

		if connectResp.PID != "" {
			s.pid = connectResp.PID
		}

	case sioEvent:
		s.dispatchPacket(pkt)

	case sioAck:
		if pkt.ID != nil {
			s.handleAck(*pkt.ID, pkt.Data)
		}

	case sioBinaryEvent:
		s.binaryBuf = newBinaryReconstructor(pkt.Attachments, pkt)

	case sioBinaryAck:
		s.binaryBuf = newBinaryReconstructor(pkt.Attachments, pkt)

	case sioConnectError:
		// Server rejected the connection.
		s.mu.RLock()
		cb := s.onClose
		s.mu.RUnlock()

		if cb != nil {
			go cb()
		}

	case sioDisconnect:
		return
	}
}

func (s *SocketIOConn) dispatchPacket(pkt *sioPacket) {
	var rawArgs []json.RawMessage
	if err := json.Unmarshal(pkt.Data, &rawArgs); err != nil {
		return
	}

	if len(rawArgs) == 0 {
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
		handler, ok := handlers[eventName]
		if ok && handler != nil {
			go handler(rawArgs[1:])
		}
	}

	// Also check the catch-all handler
	if catchAll, ok := handlers["*"]; ok && catchAll != nil {
		go catchAll(rawArgs)
	}

	s.mu.RUnlock()
}

func (s *SocketIOConn) handleAck(id int64, data json.RawMessage) {
	s.ackMu.Lock()

	entry, ok := s.acks[id]
	if ok {
		delete(s.acks, id)
	}

	s.ackMu.Unlock()

	if !ok {
		return
	}

	if entry.timer != nil {
		entry.timer.Stop()
	}

	var args []json.RawMessage
	if err := json.Unmarshal(data, &args); err != nil {
		return
	}

	go entry.fn(args)
}

func (s *SocketIOConn) heartbeatLoop() {
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			s.pongCh = make(chan struct{}, 1)

			if err := s.writeEIOPacket(eioPing, nil); err != nil {
				return
			}

			select {
			case <-s.pongCh:
				// Pong received in time.
			case <-time.After(s.config.PingTimeout):
				// Ping timeout — trigger reconnection.
				s.mu.Lock()
				if !s.skipReconnect {
					close(s.closed)
				}

				s.mu.Unlock()

				return

			case <-s.closed:
				return
			}
		}
	}
}

func (s *SocketIOConn) reconnectLoop() {
	for {
		select {
		case <-s.closed:
			return
		default:
		}

		delay := s.backoff.nextDuration()

		timer := time.NewTimer(delay)
		select {
		case <-s.closed:
			timer.Stop()
			return
		case <-timer.C:
		}

		s.mu.RLock()
		cb := s.onReconnecting
		attempt := s.backoff.attempts
		s.mu.RUnlock()

		if cb != nil {
			go cb(attempt)
		}

		ctx, cancel := context.WithTimeout(context.Background(), s.config.ConnectTimeout)
		conn, _, err := DialWebSocket(ctx, s.client, s.targetURL, s.mods...)

		cancel()

		if err != nil {
			if s.config.ReconnectionAttempts > 0 && s.backoff.attempts >= s.config.ReconnectionAttempts {
				s.mu.RLock()
				failCb := s.onReconnectFailed
				s.mu.RUnlock()

				if failCb != nil {
					go failCb()
				}

				return
			}

			continue
		}

		// Perform handshake on the new connection
		s.conn = conn

		// Reset closed channel for the new connection
		s.mu.Lock()
		s.closed = make(chan struct{})
		s.skipReconnect = false
		s.mu.Unlock()

		s.binaryBuf = nil

		if err := s.doHandshake(context.Background()); err != nil {
			_ = conn.Close()

			if s.config.ReconnectionAttempts > 0 && s.backoff.attempts >= s.config.ReconnectionAttempts {
				s.mu.RLock()
				failCb := s.onReconnectFailed
				s.mu.RUnlock()

				if failCb != nil {
					go failCb()
				}

				return
			}

			continue
		}

		s.stateMu.Lock()
		s.state = sioStateOpen
		s.stateMu.Unlock()

		s.backoff.reset()

		s.mu.RLock()
		reconnectedCb := s.onReconnected
		s.mu.RUnlock()

		if reconnectedCb != nil {
			go reconnectedCb()
		}

		go s.readLoop()
		go s.heartbeatLoop()

		return
	}
}

func (s *SocketIOConn) readEIOPacket() (byte, []byte, error) {
	data, err := io.ReadAll(s.conn)
	if err != nil {
		return 0, nil, err
	}

	if len(data) == 0 {
		return 0, nil, io.EOF
	}

	return data[0], data[1:], nil
}

func readEIOPacketCtx(ctx context.Context, conn net.Conn) (byte, []byte, error) {
	type result struct {
		pType   byte
		payload []byte
		err     error
	}

	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(conn)
		if err != nil {
			ch <- result{err: err}
			return
		}

		if len(data) == 0 {
			ch <- result{err: io.EOF}
			return
		}

		ch <- result{pType: data[0], payload: data[1:]}
	}()

	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case r := <-ch:
		return r.pType, r.payload, r.err
	}
}

func (s *SocketIOConn) writeEIOPacket(pType byte, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	data := make([]byte, 1+len(payload))
	data[0] = pType
	copy(data[1:], payload)

	_, err := s.conn.Write(data)

	return err
}

// sioPacket represents a decoded Socket.IO v5 packet.
type sioPacket struct {
	Type        byte
	Namespace   string
	ID          *int64
	Attachments int
	Data        json.RawMessage
}

type ackEntry struct {
	fn    func(args []json.RawMessage)
	timer *time.Timer
}

type binaryReconstructor struct {
	attachments int
	buffers     [][]byte
	packet      *sioPacket
}

func newBinaryReconstructor(attachments int, pkt *sioPacket) *binaryReconstructor {
	return &binaryReconstructor{
		attachments: attachments,
		packet:      pkt,
	}
}

func (br *binaryReconstructor) addBuffer(data []byte) bool {
	br.buffers = append(br.buffers, data)
	return len(br.buffers) >= br.attachments
}

func (br *binaryReconstructor) reconstruct() (*sioPacket, error) {
	if len(br.buffers) != br.attachments {
		return nil, fmt.Errorf("aoni sio: expected %d attachments, got %d", br.attachments, len(br.buffers))
	}

	pkt := *br.packet

	var rawArgs []json.RawMessage
	if err := json.Unmarshal(pkt.Data, &rawArgs); err != nil {
		return nil, fmt.Errorf("aoni sio: unmarshal binary packet: %w", err)
	}

	var data []any
	for _, raw := range rawArgs {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("aoni sio: unmarshal binary arg: %w", err)
		}

		data = append(data, reconstructBinary(v, br.buffers))
	}

	pkt.Data, _ = json.Marshal(data)
	pkt.Type = sioEvent

	return &pkt, nil
}

type backoff struct {
	min      time.Duration
	max      time.Duration
	factor   float64
	jitter   float64
	attempts int
}

func newBackoff(cfg SocketIOConfig) *backoff {
	return &backoff{
		min:    cfg.ReconnectionDelay,
		max:    cfg.ReconnectionDelayMax,
		factor: 2,
		jitter: cfg.JitterFactor,
	}
}

func (b *backoff) nextDuration() time.Duration {
	ms := float64(b.min.Milliseconds()) * math.Pow(b.factor, float64(b.attempts))

	if b.jitter > 0 {
		deviation := math.Floor(rand.Float64() * b.jitter * ms)
		if rand.IntN(2) == 0 {
			ms -= deviation
		} else {
			ms += deviation
		}
	}

	b.attempts++

	if ms > float64(b.max.Milliseconds()) {
		ms = float64(b.max.Milliseconds())
	}

	if ms < 0 {
		ms = 0
	}

	return time.Duration(ms) * time.Millisecond
}

func (b *backoff) reset() {
	b.attempts = 0
}

func encodeSIOPacket(pkt sioPacket) []byte {
	var sb strings.Builder

	sb.WriteByte(pkt.Type)

	if pkt.Type == sioBinaryEvent || pkt.Type == sioBinaryAck {
		sb.WriteString(strconv.Itoa(pkt.Attachments))
		sb.WriteByte('-')
	}

	if pkt.Namespace != "" && pkt.Namespace != "/" {
		sb.WriteString(pkt.Namespace)
		sb.WriteByte(',')
	}

	if pkt.ID != nil {
		sb.WriteString(strconv.FormatInt(*pkt.ID, 10))
	}

	if pkt.Data != nil {
		sb.Write(pkt.Data)
	}

	return []byte(sb.String())
}

func decodeSIOPacket(data []byte) (*sioPacket, error) {
	if len(data) == 0 {
		return nil, errors.New("aoni sio: empty packet")
	}

	pkt := &sioPacket{}
	i := 0

	// Packet type
	pkt.Type = data[i]
	i++

	// Binary attachment count
	if pkt.Type == sioBinaryEvent || pkt.Type == sioBinaryAck {
		start := i
		for i < len(data) && data[i] != '-' {
			i++
		}

		pkt.Attachments, _ = strconv.Atoi(string(data[start:i]))
		if i < len(data) {
			i++ // skip '-'
		}
	}

	// Namespace
	if i < len(data) && data[i] == '/' {
		start := i
		for i < len(data) && data[i] != ',' {
			i++
		}

		pkt.Namespace = string(data[start:i])
		if i < len(data) {
			i++ // skip ','
		}
	} else {
		pkt.Namespace = "/"
	}

	// ACK ID (digits only)
	if i < len(data) && data[i] >= '0' && data[i] <= '9' {
		start := i
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}

		id, _ := strconv.ParseInt(string(data[start:i]), 10, 64)
		pkt.ID = &id
	}

	// Data (remaining bytes)
	if i < len(data) {
		pkt.Data = make(json.RawMessage, len(data)-i)
		copy(pkt.Data, data[i:])
	}

	return pkt, nil
}

func isBinary(obj any) bool {
	_, ok := obj.([]byte)
	return ok
}

func hasBinary(obj any) bool {
	switch v := obj.(type) {
	case []byte:
		return true
	case []any:
		if slices.ContainsFunc(v, hasBinary) {
			return true
		}
	case map[string]any:
		for _, val := range v {
			if hasBinary(val) {
				return true
			}
		}

	case map[string]json.RawMessage:
		for _, val := range v {
			if hasBinaryRaw(val) {
				return true
			}
		}
	}

	return false
}

func hasBinaryRaw(raw json.RawMessage) bool {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}

	return hasBinary(v)
}

func deconstructBinary(data any) (any, [][]byte) {
	var buffers [][]byte

	result := deconstructBinaryWithOffset(data, &buffers)

	return result, buffers
}

func deconstructBinaryWithOffset(data any, buffers *[][]byte) any {
	switch v := data.(type) {
	case []byte:
		idx := len(*buffers)
		*buffers = append(*buffers, v)

		return map[string]any{"_placeholder": true, "num": idx}

	case []any:
		result := make([]any, len(v))

		for i, item := range v {
			result[i] = deconstructBinaryWithOffset(item, buffers)
		}

		return result

	case map[string]any:
		result := make(map[string]any, len(v))

		for key, val := range v {
			result[key] = deconstructBinaryWithOffset(val, buffers)
		}

		return result
	}

	return data
}

func reconstructBinary(data any, buffers [][]byte) any {
	switch v := data.(type) {
	case map[string]any:
		if ph, ok := v["_placeholder"]; ok && ph == true {
			idx := -1

			switch n := v["num"].(type) {
			case float64:
				idx = int(n)
			case int:
				idx = n
			case json.Number:
				val, _ := n.Int64()
				idx = int(val)
			}

			if idx >= 0 && idx < len(buffers) {
				return buffers[idx]
			}
		}

		result := make(map[string]any, len(v))
		for key, val := range v {
			result[key] = reconstructBinary(val, buffers)
		}

		return result

	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = reconstructBinary(item, buffers)
		}

		return result
	}

	return data
}
