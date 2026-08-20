// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package connector manages transport dialing, active connection state, and resilient exponential backoff reconnect cycles.
package connector

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/async/log"

	"github.com/lemon4ksan/aoni/realtime/socket"
)

type reconnectKeyType struct{}

var reconnectKey = reconnectKeyType{}

var (
	// ErrClosed indicates an operation was attempted on a permanently closed Connector.
	ErrClosed = errors.New("connector: instance is permanently closed")
	// ErrDisconnected indicates sending failed because no active transport connection exists.
	ErrDisconnected = errors.New("connector: not connected")
	// ErrAlreadyConnecting indicates a dial attempt is already actively in progress.
	ErrAlreadyConnecting = errors.New("connector: connection attempt already in progress")
	// ErrReconnectionFailed indicates all exponential backoff reconnect attempts were exhausted.
	ErrReconnectionFailed = errors.New("connector: reconnection failed after maximum attempts")
)

// Connection defines the standard stream transport interface.
type Connection interface {
	Send(ctx context.Context, data []byte) error
	Receive(ctx context.Context) (*socket.FrameBuffer, error)
	Close() error
}

// NetConnWrapper adapts standard net.Conn and socket.Framer into a Connection.
type NetConnWrapper struct {
	conn     net.Conn
	framer   socket.Framer
	cipherMu sync.RWMutex
	cipher   socket.Cipher
}

// NewNetConnWrapper constructs a NetConnWrapper.
func NewNetConnWrapper(conn net.Conn, framer socket.Framer, cipher socket.Cipher) *NetConnWrapper {
	return &NetConnWrapper{
		conn:   conn,
		framer: framer,
		cipher: cipher,
	}
}

// SetCipher dynamically sets or updates the encryption cipher.
func (n *NetConnWrapper) SetCipher(c socket.Cipher) {
	n.cipherMu.Lock()
	n.cipher = c
	n.cipherMu.Unlock()
}

func (n *NetConnWrapper) getCipher() socket.Cipher {
	n.cipherMu.RLock()
	defer n.cipherMu.RUnlock()

	return n.cipher
}

// Send encrypts (if cipher is present) and writes a framed payload to the connection.
func (n *NetConnWrapper) Send(_ context.Context, data []byte) error {
	cipher := n.getCipher()
	if cipher != nil {
		fb := socket.AcquireFrameBuffer(len(data))
		copy(fb.Bytes(), data)

		enc, err := cipher.Encrypt(fb)
		if err != nil {
			socket.ReleaseFrameBuffer(fb)
			return fmt.Errorf("cipher encrypt: %w", err)
		}

		err = n.framer.WriteFrame(n.conn, enc.Bytes())

		socket.ReleaseFrameBuffer(fb)

		if enc != fb {
			socket.ReleaseFrameBuffer(enc)
		}

		return err
	}

	return n.framer.WriteFrame(n.conn, data)
}

// Receive reads a frame and decrypts it if cipher is present.
func (n *NetConnWrapper) Receive(_ context.Context) (*socket.FrameBuffer, error) {
	fb, err := n.framer.ReadFrame(n.conn)
	if err != nil {
		return nil, err
	}

	cipher := n.getCipher()
	if cipher != nil {
		dec, err := cipher.Decrypt(fb)
		if err != nil {
			socket.ReleaseFrameBuffer(fb)
			return nil, fmt.Errorf("cipher decrypt: %w", err)
		}

		if dec != fb {
			socket.ReleaseFrameBuffer(fb)
		}

		return dec, nil
	}

	return fb, nil
}

// Close closes the underlying net.Conn.
func (n *NetConnWrapper) Close() error {
	if n.conn != nil {
		return n.conn.Close()
	}

	return nil
}

// Dialer defines the signature for establishing network connections for a given endpoint.
type Dialer[Endpoint any] func(ctx context.Context, endpoint Endpoint, framer socket.Framer, cipher socket.Cipher) (Connection, error)

// ReconnectPolicy configures automatic retry backoffs and endpoint selection strategies.
type ReconnectPolicy[Endpoint any] struct {
	MaxAttempts      int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	BackoffFactor    float64
	Jitter           bool
	EndpointSelector func([]Endpoint) (Endpoint, bool)
}

// DefaultReconnectPolicy provides an exponential backoff policy with randomized selection.
func DefaultReconnectPolicy[Endpoint any]() ReconnectPolicy[Endpoint] {
	return ReconnectPolicy[Endpoint]{
		MaxAttempts:    0, // Infinite retries
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		Jitter:         true,
		EndpointSelector: func(endpoints []Endpoint) (Endpoint, bool) {
			if len(endpoints) == 0 {
				var zero Endpoint
				return zero, false
			}

			return endpoints[rand.IntN(len(endpoints))], true
		},
	}
}

// Config configures dialer, framer, cipher, timeouts, and reconnect strategies.
type Config[Endpoint any] struct {
	Dialer          Dialer[Endpoint]
	Framer          socket.Framer
	Cipher          socket.Cipher
	ReconnectPolicy ReconnectPolicy[Endpoint]
	ConnectTimeout  time.Duration
	Logger          log.Logger
}

// Connector maintains active socket connection states and executes automatic reconnections on transport failures.
type Connector[Endpoint any] struct {
	cfg    Config[Endpoint]
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	closed atomic.Bool

	logger   log.Logger
	incoming chan *socket.FrameBuffer

	conn            Connection
	cipher          socket.Cipher
	isConnecting    atomic.Bool
	reconnectCancel context.CancelFunc
	lastEndpoint    Endpoint
	endpoints       []Endpoint
	onReconnect     func(ctx context.Context)
}

// New constructs a generic Connector instance.
func New[Endpoint any](cfg Config[Endpoint]) *Connector[Endpoint] {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec

	l := cfg.Logger
	if l == nil {
		l = log.Discard
	}

	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 20 * time.Second
	}

	if cfg.ReconnectPolicy.InitialBackoff == 0 {
		cfg.ReconnectPolicy = DefaultReconnectPolicy[Endpoint]()
	}

	return &Connector[Endpoint]{
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		incoming:  make(chan *socket.FrameBuffer, 1024),
		logger:    l.With(log.Component("connector")),
		endpoints: make([]Endpoint, 0),
		cipher:    cfg.Cipher,
	}
}

// Done returns a channel closed when the connector is permanently shutdown.
func (c *Connector[Endpoint]) Done() <-chan struct{} { return c.ctx.Done() }

// C returns the receive channel streaming inbound frame buffers.
func (c *Connector[Endpoint]) C() <-chan *socket.FrameBuffer { return c.incoming }

// IsConnected reports whether an active connection exists.
func (c *Connector[Endpoint]) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.conn != nil && !c.closed.Load()
}

// SetCipher sets or updates the active encryption cipher.
func (c *Connector[Endpoint]) SetCipher(cipher socket.Cipher) {
	c.mu.Lock()
	c.cipher = cipher
	conn := c.conn
	c.mu.Unlock()

	if cipherSetter, ok := conn.(interface{ SetCipher(socket.Cipher) }); ok {
		cipherSetter.SetCipher(cipher)
	}
}

// SetOnReconnect registers a callback function executed after a successful reconnect cycle.
func (c *Connector[Endpoint]) SetOnReconnect(fn func(ctx context.Context)) {
	c.mu.Lock()
	c.onReconnect = fn
	c.mu.Unlock()
}

// UpdateEndpoints updates the known endpoint pool for auto-reconnection.
func (c *Connector[Endpoint]) UpdateEndpoints(endpoints []Endpoint) {
	c.mu.Lock()
	c.endpoints = endpoints
	c.mu.Unlock()
}

// Connect dials a specific endpoint.
func (c *Connector[Endpoint]) Connect(ctx context.Context, endpoint Endpoint) error {
	if c.closed.Load() {
		return ErrClosed
	}

	if ctx.Value(reconnectKey) == nil {
		c.cancelReconnect()
	}

	if !c.isConnecting.CompareAndSwap(false, true) {
		return ErrAlreadyConnecting
	}

	defer c.isConnecting.Store(false)

	c.mu.RLock()
	dialer := c.cfg.Dialer
	framer := c.cfg.Framer
	cipher := c.cipher
	c.mu.RUnlock()

	if dialer == nil {
		return errors.New("connector: no dialer configured")
	}

	connCtx, connCancel := context.WithTimeout(ctx, c.cfg.ConnectTimeout)
	defer connCancel()

	conn, err := dialer(connCtx, endpoint, framer, cipher)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}

	c.conn = conn
	c.lastEndpoint = endpoint
	onRec := c.onReconnect
	c.mu.Unlock()

	go c.readLoop(conn)

	if ctx.Value(reconnectKey) != nil && onRec != nil {
		go onRec(c.ctx)
	}

	return nil
}

func (c *Connector[Endpoint]) readLoop(conn Connection) {
	defer func() {
		c.mu.Lock()

		isSame := (c.conn == conn)
		if isSame {
			c.conn = nil
		}

		c.mu.Unlock()

		if isSame {
			_ = conn.Close()
		}

		if !c.closed.Load() {
			c.triggerReconnect()
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		fb, err := conn.Receive(c.ctx)
		if err != nil {
			return
		}

		select {
		case c.incoming <- fb:
		case <-c.ctx.Done():
			socket.ReleaseFrameBuffer(fb)
			return
		}
	}
}

// Send transmits data over the active connection.
func (c *Connector[Endpoint]) Send(ctx context.Context, data []byte) error {
	if c.closed.Load() {
		return ErrClosed
	}

	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return ErrDisconnected
	}

	return conn.Send(ctx, data)
}

// Disconnect gracefully terminates the active connection.
func (c *Connector[Endpoint]) Disconnect() error {
	c.cancelReconnect()

	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		return conn.Close()
	}

	return nil
}

// Close permanently shuts down the connector and cancels all background workers.
func (c *Connector[Endpoint]) Close() error {
	if c.closed.Swap(true) {
		return nil
	}

	c.cancelReconnect()
	c.cancel()

	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		return conn.Close()
	}

	return nil
}

func (c *Connector[Endpoint]) cancelReconnect() {
	c.mu.Lock()
	if c.reconnectCancel != nil {
		c.reconnectCancel()
		c.reconnectCancel = nil
	}

	c.mu.Unlock()
}

func (c *Connector[Endpoint]) triggerReconnect() {
	c.mu.Lock()
	if c.reconnectCancel != nil || c.closed.Load() {
		c.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(c.ctx) //nolint:gosec
	c.reconnectCancel = cancel
	policy := c.cfg.ReconnectPolicy
	c.mu.Unlock()

	go c.reconnectLoop(ctx, policy)
}

func (c *Connector[Endpoint]) reconnectLoop(ctx context.Context, policy ReconnectPolicy[Endpoint]) {
	defer c.cancelReconnect()

	backoff := policy.InitialBackoff
	attempts := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if policy.MaxAttempts > 0 && attempts >= policy.MaxAttempts {
			return
		}

		c.mu.RLock()
		endpoints := c.endpoints
		selector := policy.EndpointSelector

		c.mu.RUnlock()

		target, ok := selector(endpoints)
		if !ok {
			c.mu.RLock()
			target = c.lastEndpoint
			c.mu.RUnlock()
		}

		rCtx := context.WithValue(ctx, reconnectKey, true)
		if err := c.Connect(rCtx, target); err == nil {
			return
		}

		attempts++

		jitterVal := time.Duration(0)
		if policy.Jitter {
			jitterVal = time.Duration(rand.Int64N(int64(backoff) / 2))
		}

		sleepDur := min(backoff+jitterVal, policy.MaxBackoff)

		select {
		case <-time.After(sleepDur):
		case <-ctx.Done():
			return
		}

		factor := policy.BackoffFactor
		if factor <= 1.0 {
			factor = 2.0
		}

		backoff = min(time.Duration(float64(backoff)*factor), policy.MaxBackoff)
	}
}
