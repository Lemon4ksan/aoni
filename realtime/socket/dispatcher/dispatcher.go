// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dispatcher routes incoming parsed packets to registered numerical opcodes, string method handlers, and async job callbacks.
package dispatcher

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/foundation/async/log"
	"github.com/lemon4ksan/foundation/async/task"
)

var (
	// ErrClosed indicates operation was attempted on a closed Dispatcher.
	ErrClosed = errors.New("dispatcher: closed")
	// ErrJobTimeout indicates synchronous RPC timed out.
	ErrJobTimeout = errors.New("dispatcher: request timed out waiting for response")
)

// Handler processes a fully parsed packet.
type Handler[Packet any] func(packet Packet)

// Writer writes serialized byte slices to the outbound transport.
type Writer interface {
	Send(ctx context.Context, data []byte) error
}

// Extractor extracts routing identifiers (OpCode, Method, JobID) from a decoded packet.
type Extractor[OpCode comparable, JobID comparable, Packet any] struct {
	GetOpCode func(pkt Packet) OpCode
	GetMethod func(pkt Packet) (string, bool)
	GetJobID  func(pkt Packet) (JobID, bool)
}

// Config configures the Dispatcher.
type Config struct {
	MaxJobs int
	Logger  log.Logger
}

// DefaultConfig builds default dispatcher settings.
func DefaultConfig() Config {
	return Config{
		MaxJobs: 1000,
		Logger:  log.Discard,
	}
}

// Dispatcher manages numerical and method-based packet routing and correlates synchronous request-response task.
type Dispatcher[OpCode comparable, JobID comparable, Packet any] struct {
	cfg       Config
	writer    Writer
	extractor Extractor[OpCode, JobID, Packet]
	tasks     *task.Manager[JobID, Packet]
	logger    log.Logger

	mu             sync.RWMutex
	opcodeHandlers map[OpCode]Handler[Packet]
	methodHandlers map[string]Handler[Packet]

	jobCounter atomic.Uint64
	closed     atomic.Bool
}

// New constructs a generic Dispatcher instance.
func New[OpCode, JobID comparable, Packet any](
	cfg Config,
	writer Writer,
	extractor Extractor[OpCode, JobID, Packet],
) *Dispatcher[OpCode, JobID, Packet] {
	l := cfg.Logger
	if l == nil {
		l = log.Discard
	}

	maxJ := cfg.MaxJobs
	if maxJ <= 0 {
		maxJ = 1000
	}

	return &Dispatcher[OpCode, JobID, Packet]{
		cfg:            cfg,
		writer:         writer,
		extractor:      extractor,
		tasks:          task.NewManager[JobID, Packet](maxJ),
		logger:         l.With(log.Component("dispatcher")),
		opcodeHandlers: make(map[OpCode]Handler[Packet]),
		methodHandlers: make(map[string]Handler[Packet]),
	}
}

// NextJobID generates a monotonically increasing numeric sequence for tracking requests.
func (d *Dispatcher[OpCode, JobID, Packet]) NextJobID() uint64 {
	return d.jobCounter.Add(1)
}

// RegisterHandler registers a packet handler for a specific numerical or comparable OpCode.
func (d *Dispatcher[OpCode, JobID, Packet]) RegisterHandler(op OpCode, h Handler[Packet]) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if h == nil {
		delete(d.opcodeHandlers, op)
		return
	}

	d.opcodeHandlers[op] = h
}

// RegisterMethodHandler registers a handler for string-based unified service methods.
func (d *Dispatcher[OpCode, JobID, Packet]) RegisterMethodHandler(method string, h Handler[Packet]) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if h == nil {
		delete(d.methodHandlers, method)
		return
	}

	d.methodHandlers[method] = h
}

// ClearHandlers removes all registered opcode and method handlers.
func (d *Dispatcher[OpCode, JobID, Packet]) ClearHandlers() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.opcodeHandlers = make(map[OpCode]Handler[Packet])
	d.methodHandlers = make(map[string]Handler[Packet])
}

// SendSync registers a JobID callback, writes the payload, and blocks until the corresponding response packet arrives or ctx cancels.
func (d *Dispatcher[OpCode, JobID, Packet]) SendSync(
	ctx context.Context,
	jobID JobID,
	payload []byte,
) (Packet, error) {
	if d.closed.Load() {
		var zero Packet
		return zero, ErrClosed
	}

	type result struct {
		pkt Packet
		err error
	}

	resCh := make(chan result, 1)
	cb := func(_ context.Context, pkt Packet, err error) {
		resCh <- result{pkt: pkt, err: err}
	}

	// Register job in task.Manager
	_ = d.tasks.Add(jobID, cb, task.WithContext[Packet](ctx))

	var zero Packet
	defer d.tasks.Resolve(jobID, zero, task.ErrJobCancelled)

	if err := d.writer.Send(ctx, payload); err != nil {
		return zero, err
	}

	select {
	case <-ctx.Done():
		return zero, ctx.Err()

	case res := <-resCh:
		if errors.Is(res.err, task.ErrJobCancelled) {
			if ctx.Err() != nil {
				return zero, ctx.Err()
			}

			return zero, task.ErrJobCancelled
		}

		return res.pkt, res.err
	}
}

// Dispatch inspects incoming packets, fulfills matching Job callbacks, and dispatches to registered handlers.
func (d *Dispatcher[OpCode, JobID, Packet]) Dispatch(packet Packet) bool {
	// 1. Check Job Correlation
	if d.extractor.GetJobID != nil {
		if jobID, ok := d.extractor.GetJobID(packet); ok {
			if d.tasks.Resolve(jobID, packet, nil) {
				return true
			}
		}
	}

	// 2. Check String Method Handler
	if d.extractor.GetMethod != nil {
		if method, ok := d.extractor.GetMethod(packet); ok && method != "" {
			d.mu.RLock()
			h, exists := d.methodHandlers[method]
			d.mu.RUnlock()

			if exists && h != nil {
				h(packet)
				return true
			}
		}
	}

	// 3. Check OpCode Handler
	if d.extractor.GetOpCode != nil {
		op := d.extractor.GetOpCode(packet)
		d.mu.RLock()
		h, exists := d.opcodeHandlers[op]
		d.mu.RUnlock()

		if exists && h != nil {
			h(packet)
			return true
		}
	}

	return false
}

// Close cancels all pending jobs and shuts down the dispatcher.
func (d *Dispatcher[OpCode, JobID, Packet]) Close() error {
	if d.closed.Swap(true) {
		return nil
	}

	d.tasks.CancelAll(task.ErrJobCancelled)

	return nil
}
