// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socketio provides client implementation for Socket.IO v5 and Engine.IO v4 over WebSockets.
package socketio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/jobs"
	"github.com/lemon4ksan/miyako/kata"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/realtime/ws"
)

var (
	// ErrNotConnected is returned when attempting to emit events on an inactive Socket.IO connection.
	ErrNotConnected = errors.New("aoni sio: connection closed or not connected")

	// ErrAckTimeout is returned when a server acknowledgment is not received within the configured deadline.
	ErrAckTimeout = errors.New("aoni sio: acknowledgment timeout")

	// ErrEmptyPacket is returned when attempting to decode a zero-length Socket.IO payload frame.
	ErrEmptyPacket = errors.New("aoni sio: empty packet")
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

type sioEventType int

const (
	sioEventTypeOpen sioEventType = iota
	sioEventTypeClose
	sioEventTypeReconnect
)

// BackoffStrategy defines the interface for calculating reconnect delays.
type BackoffStrategy interface {
	Next() time.Duration
	Reset()
}

// Config holds configuration options for Socket.IO connections.
type Config struct {
	Reconnection         bool
	ReconnectionAttempts int
	ReconnectionDelay    time.Duration
	ReconnectionDelayMax time.Duration
	JitterFactor         float64
	Backoff              BackoffStrategy
	ConnectTimeout       time.Duration
	PingTimeout          time.Duration
	Namespace            string
	Auth                 any
}

// ResolveDefaults fills zero-valued config fields with production defaults.
func (cfg *Config) ResolveDefaults() {
	cfg.ReconnectionDelay = generic.Coalesce(cfg.ReconnectionDelay, time.Second)
	cfg.ReconnectionDelayMax = generic.Coalesce(cfg.ReconnectionDelayMax, 30*time.Second)
	cfg.JitterFactor = generic.Coalesce(cfg.JitterFactor, 0.5)
	cfg.ConnectTimeout = generic.Coalesce(cfg.ConnectTimeout, 20*time.Second)
	cfg.PingTimeout = generic.Coalesce(cfg.PingTimeout, 20*time.Second)
	cfg.Namespace = generic.Coalesce(cfg.Namespace, "/")
}

// NamespaceSocket is a namespace-scoped event emitter.
type NamespaceSocket struct {
	conn *Conn
	nsp  string
}

// On registers an event handler for event on this namespace.
func (ns *NamespaceSocket) On(event string, handler func(args []json.RawMessage)) {
	ns.conn.setNamespaceHandler(ns.nsp, event, handler)
}

// OnAny registers a catch-all event listener on this namespace.
func (ns *NamespaceSocket) OnAny(handler func(event string, args []json.RawMessage)) {
	ns.conn.setNamespaceHandler(ns.nsp, "*", func(args []json.RawMessage) {
		if len(args) == 0 {
			return
		}

		var eventName string
		if err := json.Unmarshal(args[0], &eventName); err == nil {
			handler(eventName, args[1:])
		}
	})
}

// Emit sends an event on this namespace.
func (ns *NamespaceSocket) Emit(event string, args ...any) error {
	return ns.conn.emitNS(ns.nsp, event, args...)
}

// EmitWithAck sends an event and blocks until acknowledgment is received or ctx expires.
func (ns *NamespaceSocket) EmitWithAck(ctx context.Context, event string, args ...any) ([]json.RawMessage, error) {
	return ns.conn.emitWithAckNS(ctx, ns.nsp, event, args...)
}

// EmitVolatile sends an event only if currently connected, silently dropping it otherwise.
func (ns *NamespaceSocket) EmitVolatile(event string, args ...any) error {
	return ns.conn.emitVolatileNS(ns.nsp, event, args...)
}

// Conn manages Socket.IO v5 / Engine.IO v4 client connections over WebSockets.
type Conn struct {
	conn atomic.Pointer[net.Conn]
	sid  string

	writeMu sync.Mutex
	mu      sync.RWMutex
	closed  chan struct{}

	config    Config
	namespace string

	nsEvents map[string]map[string]func(args []json.RawMessage)
	onClose  func()

	onReconnecting    func(attempt int)
	onReconnected     func()
	onReconnectFailed func()

	ackMgr *jobs.Manager[int64, []json.RawMessage]

	state   sioConnState
	stateMu sync.RWMutex
	fsm     *kata.FSM[sioConnState, sioEventType]

	backoff       *generic.Backoff
	attemptCount  int
	skipReconnect bool
	reconnectStop chan struct{}
	client        *aoni.Client
	targetURL     string
	mods          []aoni.RequestModifier
	pingInterval  time.Duration

	pongMu sync.Mutex
	pongCh chan struct{}

	binaryBuf *binaryReconstructor

	pid    string
	offset string
}

// NewSocketIOConn initializes Engine.IO v4 and Socket.IO v5 protocols over an existing [net.Conn].
func NewSocketIOConn(ctx context.Context, conn net.Conn, config Config) (*Conn, error) {
	config.ResolveDefaults()

	sio := &Conn{
		config:        config,
		namespace:     config.Namespace,
		nsEvents:      make(map[string]map[string]func(args []json.RawMessage)),
		ackMgr:        jobs.NewManager[int64, []json.RawMessage](0),
		closed:        make(chan struct{}),
		reconnectStop: make(chan struct{}),
		backoff:       newBackoff(config),
		fsm:           initFSM(),
	}
	sio.conn.Store(&conn)

	if err := sio.doHandshake(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	_ = sio.fsm.Transition(context.Background(), sioEventTypeOpen)

	sio.stateMu.Lock()
	sio.state = sioStateOpen
	sio.stateMu.Unlock()

	go sio.readLoop()
	go sio.heartbeatLoop()

	return sio, nil
}

func initFSM() *kata.FSM[sioConnState, sioEventType] {
	fsm := kata.NewFSM[sioConnState, sioEventType](sioStateClosed)
	fsm.AddRules(
		kata.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateClosed,
			Event: sioEventTypeOpen,
			To:    sioStateOpen,
		},
		kata.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateOpen,
			Event: sioEventTypeClose,
			To:    sioStateClosing,
		},
		kata.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateClosed,
			Event: sioEventTypeReconnect,
			To:    sioStateOpen,
		},
		kata.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateOpening,
			Event: sioEventTypeOpen,
			To:    sioStateOpen,
		},
		kata.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateOpening,
			Event: sioEventTypeClose,
			To:    sioStateClosed,
		},
		kata.TransitionRule[sioConnState, sioEventType]{
			From:  sioStateClosing,
			Event: sioEventTypeClose,
			To:    sioStateClosed,
		},
	)

	return fsm
}

// DialSocketIO connects to a Socket.IO v5 server via the aoni uTLS WebSocket pipeline.
func DialSocketIO(
	ctx context.Context,
	c *aoni.Client,
	targetURL string,
	config Config,
	mods ...aoni.RequestModifier,
) (*Conn, error) {
	conn, _, err := ws.DialWebSocket(ctx, c, targetURL, mods...) //nolint:bodyclose
	if err != nil {
		return nil, fmt.Errorf("aoni sio: dial websocket: %w", err)
	}

	sio, err := NewSocketIOConn(ctx, conn, config)
	if err != nil {
		return nil, err
	}

	sio.client = c
	sio.targetURL = targetURL
	sio.mods = mods

	return sio, nil
}

// ReconnectionAttempts returns current consecutive reconnection attempts count.
func (s *Conn) ReconnectionAttempts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.attemptCount
}

// On registers an event handler for event on the default namespace.
func (s *Conn) On(event string, handler func(args []json.RawMessage)) {
	s.setNamespaceHandler(s.namespace, event, handler)
}

// OnAny registers a catch-all event listener on the default namespace.
func (s *Conn) OnAny(handler func(event string, args []json.RawMessage)) {
	s.setNamespaceHandler(s.namespace, "*", func(args []json.RawMessage) {
		if len(args) == 0 {
			return
		}

		var eventName string
		if err := json.Unmarshal(args[0], &eventName); err == nil {
			handler(eventName, args[1:])
		}
	})
}

// OnNamespace returns a [NamespaceSocket] scoped to nsp.
func (s *Conn) OnNamespace(nsp string) *NamespaceSocket {
	s.setNamespaceHandler(nsp, "", nil)
	return &NamespaceSocket{conn: s, nsp: nsp}
}

// OnClose registers a callback invoked when the connection terminates.
func (s *Conn) OnClose(handler func()) {
	s.mu.Lock()
	s.onClose = handler
	s.mu.Unlock()
}

// OnReconnecting registers a callback invoked before each reconnection attempt.
func (s *Conn) OnReconnecting(handler func(attempt int)) {
	s.mu.Lock()
	s.onReconnecting = handler
	s.mu.Unlock()
}

// OnReconnected registers a callback invoked after a successful reconnection.
func (s *Conn) OnReconnected(handler func()) {
	s.mu.Lock()
	s.onReconnected = handler
	s.mu.Unlock()
}

// OnReconnectFailed registers a callback invoked when reconnection attempts are exhausted.
func (s *Conn) OnReconnectFailed(handler func()) {
	s.mu.Lock()
	s.onReconnectFailed = handler
	s.mu.Unlock()
}

// Emit sends an event on the default namespace.
func (s *Conn) Emit(event string, args ...any) error {
	return s.emitNS(s.namespace, event, args...)
}

// EmitWithAck sends an event and blocks until acknowledgment is received or ctx expires.
func (s *Conn) EmitWithAck(ctx context.Context, event string, args ...any) ([]json.RawMessage, error) {
	return s.emitWithAckNS(ctx, s.namespace, event, args...)
}

// EmitVolatile sends an event only if currently connected.
func (s *Conn) EmitVolatile(event string, args ...any) error {
	return s.emitVolatileNS(s.namespace, event, args...)
}

// Close closes the Socket.IO session cleanly without triggering auto-reconnections.
func (s *Conn) Close() error {
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

	payload := encodeSIOPacket(sioPacket{
		Type:      sioDisconnect,
		Namespace: s.namespace,
	})
	_ = s.writeEIOPacket(eioMessage, payload)
	_ = s.writeEIOPacket(eioClose, nil)

	if c := s.conn.Load(); c != nil {
		return (*c).Close()
	}

	return nil
}

// SID returns the server-assigned session ID.
func (s *Conn) SID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.sid
}

func (s *Conn) getClosedChan() chan struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.closed
}

// Connected reports whether the connection is currently open.
func (s *Conn) Connected() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	return s.state == sioStateOpen
}

func (s *Conn) emitNS(nsp, event string, args ...any) error {
	if err := s.assertOpen(); err != nil {
		return err
	}

	emitArgs, ackFn := extractAckCallback(args)
	payload := make([]any, 1+len(emitArgs))
	payload[0] = event
	copy(payload[1:], emitArgs)

	if hasBinary(payload) {
		return s.emitBinaryNS(nsp, payload, ackFn)
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("aoni sio: marshal event: %w", err)
	}

	pkt := sioPacket{Type: sioEvent, Namespace: nsp, Data: jsonData}
	if ackFn != nil {
		pkt.ID = s.registerAckJob(ackFn)
	}

	return s.writeEIOPacket(eioMessage, encodeSIOPacket(pkt))
}

func (s *Conn) assertOpen() error {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	if s.state != sioStateOpen {
		return ErrNotConnected
	}

	return nil
}

func extractAckCallback(args []any) ([]any, func([]json.RawMessage)) {
	if len(args) > 0 {
		if fn, ok := args[len(args)-1].(func(args []json.RawMessage)); ok {
			return args[:len(args)-1], fn
		}
	}

	return args, nil
}

func (s *Conn) registerAckJob(ackFn func([]json.RawMessage)) *int64 {
	id := s.ackMgr.NextID()
	_ = s.ackMgr.Add(id, func(_ context.Context, response []json.RawMessage, err error) {
		if err == nil {
			ackFn(response)
		}
	}, jobs.WithTimeout[[]json.RawMessage](30*time.Second))

	return &id
}

func (s *Conn) emitBinaryNS(nsp string, data any, ackFn func(args []json.RawMessage)) error {
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
		id := s.ackMgr.NextID()
		pkt.ID = &id
		_ = s.ackMgr.Add(id, func(_ context.Context, response []json.RawMessage, err error) {
			if err == nil {
				ackFn(response)
			}
		}, jobs.WithTimeout[[]json.RawMessage](30*time.Second))
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

func (s *Conn) emitWithAckNS(ctx context.Context, nsp, event string, args ...any) ([]json.RawMessage, error) {
	ch := make(chan []json.RawMessage, 1)

	emitArgs := make([]any, len(args)+1)
	copy(emitArgs, args)
	emitArgs[len(args)] = func(rawArgs []json.RawMessage) {
		select {
		case ch <- rawArgs:
		default:
		}
	}

	if err := s.emitNS(nsp, event, emitArgs...); err != nil {
		return nil, err
	}

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.getClosedChan():
		return nil, ErrNotConnected
	case <-timer.C:
		return nil, ErrAckTimeout
	case result := <-ch:
		return result, nil
	}
}

func (s *Conn) emitVolatileNS(nsp, event string, args ...any) error {
	s.stateMu.RLock()
	connected := s.state == sioStateOpen
	s.stateMu.RUnlock()

	if !connected {
		return nil
	}

	return s.emitNS(nsp, event, args...)
}

func (s *Conn) setNamespaceHandler(nsp, event string, handler func(args []json.RawMessage)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.nsEvents[nsp] == nil {
		s.nsEvents[nsp] = make(map[string]func(args []json.RawMessage))
	}

	if event != "" {
		s.nsEvents[nsp][event] = handler
	}
}

func (s *Conn) readLoop() {
	defer s.cleanupConnection()

	for {
		pType, payload, err := s.readEIOPacket()
		if err != nil {
			return
		}

		if shouldExit := s.dispatchEIOPacket(pType, payload); shouldExit {
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

func (s *Conn) dispatchEIOPacket(pType byte, payload []byte) (shouldExit bool) {
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
	conn, _, err := ws.DialWebSocket(ctx, s.client, s.targetURL, s.mods...) //nolint:bodyclose

	cancel()

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

func (s *Conn) doHandshake(ctx context.Context) error {
	conn := s.conn.Load()
	if conn == nil {
		return ErrNotConnected
	}

	if err := s.readAndParseEIOOpen(ctx, *conn); err != nil {
		return err
	}

	if err := s.sendConnect(); err != nil {
		return fmt.Errorf("aoni sio: send connect: %w", err)
	}

	return s.readAndParseSIOConnect(ctx, *conn)
}

func (s *Conn) readAndParseEIOOpen(ctx context.Context, conn net.Conn) error {
	pType, payload, err := readEIOPacketCtx(ctx, conn)
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

	s.mu.Lock()
	s.sid = params.SID
	s.pingInterval = time.Duration(params.PingInterval) * time.Millisecond
	s.mu.Unlock()

	return nil
}

func (s *Conn) readAndParseSIOConnect(ctx context.Context, conn net.Conn) error {
	pType, payload, err := readEIOPacketCtx(ctx, conn)
	if err != nil {
		return fmt.Errorf("aoni sio: read connect response: %w", err)
	}

	if pType != eioMessage || len(payload) < 1 || payload[0] != sioConnect {
		if pType == eioMessage && len(payload) > 0 && payload[0] == sioConnectError {
			return fmt.Errorf("aoni sio: connect rejected: %s", string(payload[1:]))
		}

		return fmt.Errorf("aoni sio: unexpected connect response: %c%s", pType, string(payload))
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
				return fmt.Errorf("aoni sio: marshal auth: %w", err)
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

func (s *Conn) readEIOPacket() (byte, []byte, error) {
	conn := s.conn.Load()
	if conn == nil {
		return 0, nil, ErrNotConnected
	}

	return readSingleEIOPacket(*conn)
}

func readEIOPacketCtx(ctx context.Context, conn net.Conn) (byte, []byte, error) {
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

func readSingleEIOPacket(conn net.Conn) (byte, []byte, error) {
	if wc, ok := conn.(interface{ RawConn() *websocket.Conn }); ok {
		msgType, data, err := wc.RawConn().ReadMessage()
		if err != nil {
			return 0, nil, err
		}

		if len(data) == 0 {
			return 0, nil, io.EOF
		}

		if msgType == websocket.BinaryMessage {
			return eioBinary, data, nil
		}

		return data[0], data[1:], nil
	}

	var buf bytes.Buffer

	tmp := make([]byte, 4096)

	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}

		if buf.Len() > maxEIOPacketSize {
			return 0, nil, fmt.Errorf("aoni eio: packet too large (exceeds %d bytes)", maxEIOPacketSize)
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

func (s *Conn) writeEIOPacket(pType byte, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	conn := s.conn.Load()
	if conn == nil {
		return ErrNotConnected
	}

	data := make([]byte, 1+len(payload))
	data[0] = pType
	copy(data[1:], payload)

	_, err := (*conn).Write(data)

	return err
}

type sioPacket struct {
	Type        byte
	Namespace   string
	ID          *int64
	Attachments int
	Data        json.RawMessage
}

type binaryReconstructor struct {
	attachments int
	buffers     [][]byte
	packet      *sioPacket
}

const (
	maxBinaryAttachments = 64
	maxBinaryBufferSize  = 32 * 1024 * 1024
	maxEIOPacketSize     = 8 * 1024 * 1024
)

func newBinaryReconstructor(attachments int, pkt *sioPacket) *binaryReconstructor {
	if attachments > maxBinaryAttachments {
		attachments = maxBinaryAttachments
	}

	return &binaryReconstructor{
		attachments: attachments,
		packet:      pkt,
	}
}

func (br *binaryReconstructor) addBuffer(data []byte) bool {
	br.buffers = append(br.buffers, data)

	totalSize := 0
	for _, buf := range br.buffers {
		totalSize += len(buf)
		if totalSize > maxBinaryBufferSize {
			return true
		}
	}

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

func newBackoff(cfg Config) *generic.Backoff {
	return generic.NewBackoff(cfg.ReconnectionDelay, cfg.ReconnectionDelayMax, 2, cfg.JitterFactor)
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
		return nil, ErrEmptyPacket
	}

	pkt := &sioPacket{Type: data[0]}
	offset := 1

	if pkt.Type == sioBinaryEvent || pkt.Type == sioBinaryAck {
		attachments, err := parseAttachments(data, &offset)
		if err != nil {
			return nil, err
		}

		pkt.Attachments = attachments
	}

	pkt.Namespace = parseNamespace(data, &offset)
	pkt.ID = parseAckID(data, &offset)

	if offset < len(data) {
		pkt.Data = make(json.RawMessage, len(data)-offset)
		copy(pkt.Data, data[offset:])
	}

	return pkt, nil
}

func parseAttachments(data []byte, offset *int) (int, error) {
	start := *offset

	i := start
	for i < len(data) && data[i] != '-' {
		i++
	}

	attachments, err := strconv.Atoi(string(data[start:i]))
	if err != nil || attachments < 0 || attachments > maxBinaryAttachments {
		return 0, fmt.Errorf("aoni sio: invalid attachment count: %q", string(data[start:i]))
	}

	if i < len(data) {
		i++
	}

	*offset = i

	return attachments, nil
}

func parseNamespace(data []byte, offset *int) string {
	i := *offset
	if i < len(data) && data[i] == '/' {
		start := i
		for i < len(data) && data[i] != ',' {
			i++
		}

		nsp := string(data[start:i])
		if i < len(data) {
			i++
		}

		*offset = i

		return nsp
	}

	return "/"
}

func parseAckID(data []byte, offset *int) *int64 {
	i := *offset
	if i < len(data) && data[i] >= '0' && data[i] <= '9' {
		start := i
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}

		id, _ := strconv.ParseInt(string(data[start:i]), 10, 64)
		*offset = i

		return &id
	}

	return nil
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
