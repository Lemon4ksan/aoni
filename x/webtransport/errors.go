// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"errors"
	"fmt"
)

var (
	// ErrSessionClosed is returned when an operation is performed on a closed WebTransport session.
	ErrSessionClosed = errors.New("aoni/webtransport: session closed")

	// ErrSessionDraining is returned when attempting to open a new stream while the session is draining.
	ErrSessionDraining = errors.New("aoni/webtransport: session is draining")

	// ErrSessionGone is returned when stream operations fail because the session has terminated.
	ErrSessionGone = errors.New("aoni/webtransport: session is gone")

	// ErrInvalidCapsule is returned when a received capsule frame has invalid formatting or length.
	ErrInvalidCapsule = errors.New("aoni/webtransport: invalid capsule payload")

	// ErrHandshakeFailed is returned when the HTTP/3 Extended CONNECT handshake fails.
	ErrHandshakeFailed = errors.New("aoni/webtransport: handshake failed")

	// ErrDatagramTooLarge is returned when a datagram payload exceeds the maximum QUIC datagram size.
	ErrDatagramTooLarge = errors.New("aoni/webtransport: datagram payload too large")

	// ErrStreamRejected is returned when a stream cannot be opened or is rejected by the peer.
	ErrStreamRejected = errors.New("aoni/webtransport: stream rejected")

	// ErrStreamClosed is returned when an operation is performed on a closed stream.
	ErrStreamClosed = errors.New("aoni/webtransport: stream closed")

	// ErrFlowControl is returned when a session or stream limit is exceeded.
	ErrFlowControl = errors.New("aoni/webtransport: flow control error")

	// ErrALPN is returned when application subprotocol negotiation fails.
	ErrALPN = errors.New("aoni/webtransport: alpn negotiation failed")

	// ErrRequirementsNotMet is returned when peer SETTINGS or transport parameters do not meet requirements.
	ErrRequirementsNotMet = errors.New("aoni/webtransport: requirements not met")
)

// SessionError represents an application-level closure received in a CLOSE_WEBTRANSPORT_SESSION capsule.
type SessionError struct {
	ErrorCode uint32
	Message   string
}

// Error formats the SessionError into a human-readable string.
func (e *SessionError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("aoni/webtransport: session closed with code %d: %s", e.ErrorCode, e.Message)
	}

	return fmt.Sprintf("aoni/webtransport: session closed with code %d", e.ErrorCode)
}

// WebTransportCodeToHTTPCode maps a 32-bit WebTransport application error code
// into the reserved HTTP/3 error code space per draft-ietf-webtrans-http3-16 §4.4.
func WebTransportCodeToHTTPCode(n uint32) uint64 {
	n64 := uint64(n)
	return WTApplicationErrorFirst + n64 + (n64 / 0x1e)
}

// HTTPCodeToWebTransportCode converts an HTTP/3 error code back into a 32-bit WebTransport
// application error code per draft-ietf-webtrans-http3-16 §4.4.
// Returns (code, true) if within valid WT_APPLICATION_ERROR range and not a reserved codepoint,
// or (0, false) otherwise.
func HTTPCodeToWebTransportCode(h uint64) (uint32, bool) {
	if h < WTApplicationErrorFirst || h > WTApplicationErrorLast {
		return 0, false
	}

	if (h-0x21)%0x1f == 0 {
		return 0, false
	}

	shifted := h - WTApplicationErrorFirst

	wtCode := shifted - (shifted / 0x1f)
	if wtCode > 0xffffffff {
		return 0, false
	}

	return uint32(wtCode), true
}
