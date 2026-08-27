// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor executes parallel parsing of raw network byte frames into structured packets using multi-core worker pools.
package processor

import (
	"context"
	"runtime"
	"sync"

	"github.com/lemon4ksan/foundation/async/logkit"

	"github.com/lemon4ksan/aoni/realtime/socket"
)

// Consumer defines the destination sink for parsed packets.
type Consumer[Packet any] interface {
	Dispatch(packet Packet) bool
}

// ConsumerFunc allows plain functions to act as Consumers.
type ConsumerFunc[Packet any] func(packet Packet) bool

// Dispatch implements Consumer[Packet].
func (f ConsumerFunc[Packet]) Dispatch(packet Packet) bool {
	return f(packet)
}

// DecodeFunc parses a raw byte slice into a structured Packet instance.
type DecodeFunc[Packet any] func(data []byte) (Packet, error)

// Config configures worker count and buffering for the processor.
type Config struct {
	WorkerCount int
	Logger      logkit.Logger
}

// DefaultConfig returns recommended worker pool configuration based on CPU core count.
func DefaultConfig() Config {
	return Config{
		WorkerCount: max(runtime.NumCPU(), 2),
		Logger:      logkit.Discard,
	}
}

// Processor manages background worker goroutines to parse incoming byte frames concurrently.
type Processor[Packet any] struct {
	cfg      Config
	logger   logkit.Logger
	consumer Consumer[Packet]
	decode   DecodeFunc[Packet]

	input  <-chan *socket.FrameBuffer
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	isStarted sync.Once
	isStopped sync.Once
}

// New constructs a generic Processor instance.
func New[Packet any](
	cfg Config,
	input <-chan *socket.FrameBuffer,
	consumer Consumer[Packet],
	decode DecodeFunc[Packet],
) *Processor[Packet] {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec

	l := cfg.Logger
	if l == nil {
		l = logkit.Discard
	}

	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = max(runtime.NumCPU(), 2)
	}

	return &Processor[Packet]{
		cfg:      cfg,
		logger:   l.With(logkit.Component("processor")),
		consumer: consumer,
		decode:   decode,
		input:    input,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start spawns worker pool goroutines. Safe to call multiple times.
func (p *Processor[Packet]) Start() {
	p.isStarted.Do(func() {
		p.logger.Debug("Starting socket packet processor", logkit.Int("workers", p.cfg.WorkerCount))

		for range p.cfg.WorkerCount {
			p.wg.Go(p.worker)
		}
	})
}

// Stop gracefully terminates all worker pool goroutines.
func (p *Processor[Packet]) Stop() {
	p.isStopped.Do(func() {
		p.cancel()
		p.wg.Wait()
	})
}

func (p *Processor[Packet]) worker() {
	for {
		select {
		case <-p.ctx.Done():
			return

		case fb, ok := <-p.input:
			if !ok {
				return
			}

			if fb == nil {
				continue
			}

			p.processFrame(fb)
		}
	}
}

func (p *Processor[Packet]) processFrame(fb *socket.FrameBuffer) {
	defer socket.ReleaseFrameBuffer(fb)

	if p.decode == nil || p.consumer == nil {
		return
	}

	pkt, err := p.decode(fb.Bytes())
	if err != nil {
		p.logger.Warn("Failed to decode frame", logkit.Err(err))
		return
	}

	p.consumer.Dispatch(pkt)
}
