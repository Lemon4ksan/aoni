// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"crypto/rand"
	"errors"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/hexkit"
)

// Standard W3C TraceContext Header Names.
const (
	HeaderTraceParent = "traceparent"
	HeaderTraceState  = "tracestate"
	HeaderBaggage     = "baggage"
)

// Common W3C TraceContext errors.
var (
	ErrInvalidTraceParent = errors.New("otel: invalid W3C traceparent header")
	ErrInvalidTraceID     = errors.New("otel: invalid 16-byte TraceID")
	ErrInvalidSpanID      = errors.New("otel: invalid 8-byte SpanID")
	ErrZeroTraceID        = errors.New("otel: TraceID cannot be all zeros")
	ErrZeroSpanID         = errors.New("otel: SpanID cannot be all zeros")
)

// TraceFlags represents 8-bit W3C trace flags.
type TraceFlags byte

const (
	// FlagSampled indicates that the caller recorded the span and requested downstream recording.
	FlagSampled TraceFlags = 0x01
)

// IsSampled returns true if the sampled flag bit is set.
func (f TraceFlags) IsSampled() bool {
	return (f & FlagSampled) == FlagSampled
}

// String formats the trace flags as a 2-character hex string.
func (f TraceFlags) String() string {
	var buf [2]byte
	var src = [1]byte{byte(f)}
	hexkit.Encode(buf[:], src[:])
	return string(buf[:])
}

// TraceID is a unique 16-byte identifier representing a distributed trace.
type TraceID [16]byte

// NilTraceID represents an empty, uninitialized TraceID.
var NilTraceID = TraceID{}

// NewTraceID generates a cryptographically random, valid 16-byte TraceID.
func NewTraceID() TraceID {
	var id TraceID
	for {
		if _, err := rand.Read(id[:]); err == nil && id.IsValid() {
			return id
		}
	}
}

// ParseTraceID decodes a 32-character lowercase hex string into a TraceID.
func ParseTraceID(s string) (TraceID, error) {
	var id TraceID
	if len(s) != 32 {
		return id, ErrInvalidTraceID
	}

	if !hexkit.Decode32(&id, s) {
		return id, ErrInvalidTraceID
	}

	if !id.IsValid() {
		return id, ErrZeroTraceID
	}

	return id, nil
}

// IsValid checks whether the TraceID contains at least one non-zero byte.
func (t TraceID) IsValid() bool {
	return t != NilTraceID
}

// String encodes the TraceID as a 32-character lowercase hex string.
func (t TraceID) String() string {
	var buf [32]byte
	hexkit.Encode16(&buf, t)
	return string(buf[:])
}

// SpanID is a unique 8-byte identifier representing a single operation within a trace.
type SpanID [8]byte

// NilSpanID represents an empty, uninitialized SpanID.
var NilSpanID = SpanID{}

// NewSpanID generates a cryptographically random, valid 8-byte SpanID.
func NewSpanID() SpanID {
	var id SpanID
	for {
		if _, err := rand.Read(id[:]); err == nil && id.IsValid() {
			return id
		}
	}
}

// ParseSpanID decodes a 16-character lowercase hex string into a SpanID.
func ParseSpanID(s string) (SpanID, error) {
	var id SpanID
	if len(s) != 16 {
		return id, ErrInvalidSpanID
	}

	if !hexkit.Decode16(&id, s) {
		return id, ErrInvalidSpanID
	}

	if !id.IsValid() {
		return id, ErrZeroSpanID
	}

	return id, nil
}

// IsValid checks whether the SpanID contains at least one non-zero byte.
func (s SpanID) IsValid() bool {
	return s != NilSpanID
}

// String encodes the SpanID as a 16-character lowercase hex string.
func (s SpanID) String() string {
	var buf [16]byte
	hexkit.Encode8(&buf, s)
	return string(buf[:])
}

// SpanContext carries immutable identification information about a Span across process boundaries.
type SpanContext struct {
	traceID    TraceID
	spanID     SpanID
	traceFlags TraceFlags
	traceState string
	remote     bool
}

// NewSpanContext constructs an immutable [SpanContext].
func NewSpanContext(traceID TraceID, spanID SpanID, flags TraceFlags, traceState string, remote bool) SpanContext {
	return SpanContext{
		traceID:    traceID,
		spanID:     spanID,
		traceFlags: flags,
		traceState: traceState,
		remote:     remote,
	}
}

// TraceID returns the 16-byte TraceID.
func (sc SpanContext) TraceID() TraceID {
	return sc.traceID
}

// SpanID returns the 8-byte SpanID.
func (sc SpanContext) SpanID() SpanID {
	return sc.spanID
}

// TraceFlags returns the trace flags byte.
func (sc SpanContext) TraceFlags() TraceFlags {
	return sc.traceFlags
}

// TraceState returns the W3C tracestate string.
func (sc SpanContext) TraceState() string {
	return sc.traceState
}

// IsRemote returns true if this SpanContext was propagated from a remote parent.
func (sc SpanContext) IsRemote() bool {
	return sc.remote
}

// IsValid checks if both TraceID and SpanID are valid non-zero identifiers.
func (sc SpanContext) IsValid() bool {
	return sc.traceID.IsValid() && sc.spanID.IsValid()
}

// IsSampled returns true if the sampled flag is set.
func (sc SpanContext) IsSampled() bool {
	return sc.traceFlags.IsSampled()
}

// TraceParent formats the SpanContext into the canonical W3C traceparent header:
// "00-{trace_id}-{span_id}-{flags}" (55 bytes).
func (sc SpanContext) TraceParent() string {
	if !sc.IsValid() {
		return ""
	}

	var buf [55]byte
	buf[0] = '0'
	buf[1] = '0'
	buf[2] = '-'

	var traceBuf [32]byte
	hexkit.Encode16(&traceBuf, sc.traceID)
	copy(buf[3:35], traceBuf[:])
	buf[35] = '-'

	var spanBuf [16]byte
	hexkit.Encode8(&spanBuf, sc.spanID)
	copy(buf[36:52], spanBuf[:])
	buf[52] = '-'

	var flagsBuf [2]byte
	var flagsSrc = [1]byte{byte(sc.traceFlags)}
	hexkit.Encode(flagsBuf[:], flagsSrc[:])
	buf[53] = flagsBuf[0]
	buf[54] = flagsBuf[1]

	return string(buf[:])
}

// ParseTraceParent parses a W3C traceparent header string (e.g. "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01").
//
// In accordance with the W3C TraceContext Recommendation:
//   - Validates version format (must start with "00-" or forward compatible "xx-").
//   - Rejects headers where TraceID or SpanID are all zeroes.
//   - Rejects headers with invalid characters or incorrect part lengths.
func ParseTraceParent(header string) (SpanContext, error) {
	header = strings.TrimSpace(header)
	if len(header) < 55 {
		return SpanContext{}, ErrInvalidTraceParent
	}

	// Direct index checking for hyphens
	if header[2] != '-' || header[35] != '-' || header[52] != '-' {
		return SpanContext{}, ErrInvalidTraceParent
	}

	version := header[:2]
	if version == "ff" {
		return SpanContext{}, ErrInvalidTraceParent
	}

	// Version 00 must be exactly 55 characters total
	if version == "00" && len(header) != 55 {
		return SpanContext{}, ErrInvalidTraceParent
	}

	var traceID TraceID
	if !hexkit.Decode32(&traceID, header[3:35]) || !traceID.IsValid() {
		return SpanContext{}, ErrInvalidTraceParent
	}

	var spanID SpanID
	if !hexkit.Decode16(&spanID, header[36:52]) || !spanID.IsValid() {
		return SpanContext{}, ErrInvalidTraceParent
	}

	f1, ok1 := hexkit.FromHexChar(header[53])
	f2, ok2 := hexkit.FromHexChar(header[54])
	if !ok1 || !ok2 {
		return SpanContext{}, ErrInvalidTraceParent
	}

	return SpanContext{
		traceID:    traceID,
		spanID:     spanID,
		traceFlags: TraceFlags((f1 << 4) | f2),
		remote:     true,
	}, nil
}
