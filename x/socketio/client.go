// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socketio provides client implementation for Socket.IO v5 and Engine.IO v4 over aoni's native WebSockets.
package socketio

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/async/fsm"
	"github.com/lemon4ksan/foundation/async/task"
	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/realtime/ws"
)

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

// Conn manages Socket.IO v5 / Engine.IO v4 client connections over native WebSockets.
type Conn struct {
	conn atomic.Pointer[ws.Conn]
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

	ackMgr *task.Manager[int64, []json.RawMessage]

	state   sioConnState
	stateMu sync.RWMutex
	fsm     *fsm.FSM[sioConnState, sioEventType]

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

// NewSocketIOConn initializes Engine.IO v4 and Socket.IO v5 protocols over an established ws.Conn.
func NewSocketIOConn(ctx context.Context, conn ws.Conn, config Config) (*Conn, error) {
	config.ResolveDefaults()

	sio := &Conn{
		config:        config,
		namespace:     config.Namespace,
		nsEvents:      make(map[string]map[string]func(args []json.RawMessage)),
		ackMgr:        task.NewManager[int64, []json.RawMessage](0),
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

// DialSocketIO connects to a Socket.IO v5 server via aoni's uTLS WebSocket pipeline.
func DialSocketIO(
	ctx context.Context,
	c *aoni.Client,
	targetURL string,
	config Config,
	mods ...aoni.RequestModifier,
) (*Conn, error) {
	conn, resp, err := ws.DialWebSocket(ctx, c, targetURL, mods...)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		return nil, fmt.Errorf("aoni/socketio: dial websocket: %w", err)
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

// OnNamespace returns a NamespaceSocket scoped to nsp.
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
		return fmt.Errorf("aoni/socketio: marshal event: %w", err)
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
	}, task.WithTimeout[[]json.RawMessage](30*time.Second))

	return &id
}

func (s *Conn) emitBinaryNS(nsp string, data any, ackFn func(args []json.RawMessage)) error {
	deconstructed, buffers := deconstructBinary(data)

	jsonData, err := json.Marshal(deconstructed)
	if err != nil {
		return fmt.Errorf("aoni/socketio: marshal binary event: %w", err)
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
		}, task.WithTimeout[[]json.RawMessage](30*time.Second))
	}

	encoded := encodeSIOPacket(pkt)
	if err := s.writeEIOPacket(eioMessage, encoded); err != nil {
		return err
	}

	for _, buf := range buffers {
		if err := s.writeEIOPacket(eioBinary, buf); err != nil {
			return fmt.Errorf("aoni/socketio: send binary attachment: %w", err)
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
