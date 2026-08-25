// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"

	impl "github.com/lemon4ksan/aoni/internal/masque"
	"github.com/lemon4ksan/aoni/internal/quic"
)

var netErrClosed = net.ErrClosed

// DatagramTransport defines the interface for transmitting and receiving QUIC DATAGRAM frames
// per RFC 9221 (An Unreliable Datagram Extension to QUIC) and RFC 9297 Section 2.1 (HTTP/3 Datagrams).
type DatagramTransport interface {
	SendDatagram(p []byte) error
	ReceiveDatagram(ctx context.Context) ([]byte, error)
}

// Session represents an active MASQUE proxying session multiplexing QUIC datagrams and capsule control frames
// per RFC 9484 Section 4 (Tunnelling IP over HTTP) and RFC 9297 Section 3.2 (The Capsule Protocol).
type Session struct {
	controlStream io.ReadWriteCloser
	datagrams     DatagramTransport
	contextID     uint64
	closed        atomic.Bool
}

// NewSession initializes a new MASQUE [Session] over an underlying control stream and datagram transport.
func NewSession(controlStream io.ReadWriteCloser, datagrams DatagramTransport) *Session {
	return &Session{
		controlStream: controlStream,
		datagrams:     datagrams,
		contextID:     0,
	}
}

// SendIPPacket encapsulates and transmits an IP packet inside an HTTP Datagram payload per RFC 9484 Section 5 & 6 (Figure 13).
// Prepends Context ID 0x00 (varint for IP payloads) achieving 0 allocations via a reusable stack buffer.
func (s *Session) SendIPPacket(packet []byte) error {
	if s.closed.Load() {
		return netErrClosed
	}

	if s.datagrams == nil {
		return errors.New("aoni/masque: datagram transport not configured")
	}

	var stackBuf [2048]byte

	var buf []byte

	varIdLen := impl.EncodeVarintSlice(s.contextID, stackBuf[:8])
	totalLen := varIdLen + len(packet)

	if totalLen <= len(stackBuf) {
		buf = stackBuf[:totalLen]
	} else {
		buf = make([]byte, totalLen)
		_ = impl.EncodeVarintSlice(s.contextID, buf[:varIdLen])
	}

	copy(buf[varIdLen:], packet)

	return s.datagrams.SendDatagram(buf)
}

// ReceiveIPPacket awaits and decodes an incoming IP packet from an HTTP Datagram payload (RFC 9484 §6).
// Validates that Context ID == 0x00 (IP payload) and returns the encapsulated raw IP packet.
func (s *Session) ReceiveIPPacket(ctx context.Context) ([]byte, error) {
	if s.closed.Load() {
		return nil, netErrClosed
	}

	if s.datagrams == nil {
		return nil, errors.New("aoni/masque: datagram transport not configured")
	}

	for {
		raw, err := s.datagrams.ReceiveDatagram(ctx)
		if err != nil {
			return nil, err
		}

		if len(raw) == 0 {
			continue
		}

		ctxID, n, err := DecodeVarint(raw)
		if err != nil {
			continue
		}

		if ctxID != s.contextID {
			// Ignore unexpected context IDs per RFC 9484 §6
			continue
		}

		return raw[n:], nil
	}
}

// WriteCapsule encodes and writes a Capsule frame to the bidirectional control stream (RFC 9297 §3.2 Figure 3).
func (s *Session) WriteCapsule(capsuleType uint64, payload []byte) error {
	if s.closed.Load() {
		return netErrClosed
	}

	if s.controlStream == nil {
		return errors.New("aoni/masque: control stream not configured")
	}

	var hdrBuf [16]byte

	hdrLen := EncodeCapsuleHeader(capsuleType, uint64(len(payload)), hdrBuf[:])

	if len(payload) <= 512 {
		var combined [16 + 512]byte
		copy(combined[:hdrLen], hdrBuf[:hdrLen])
		copy(combined[hdrLen:hdrLen+len(payload)], payload)

		_, err := s.controlStream.Write(combined[:hdrLen+len(payload)])

		return err
	}

	if _, err := s.controlStream.Write(hdrBuf[:hdrLen]); err != nil {
		return err
	}

	if len(payload) > 0 {
		if _, err := s.controlStream.Write(payload); err != nil {
			return err
		}
	}

	return nil
}

// ReadCapsule reads the next Capsule frame (type, payload) from the bidirectional control stream per RFC 9297 Section 3.2.
func (s *Session) ReadCapsule() (uint64, []byte, error) {
	if s.closed.Load() {
		return 0, nil, netErrClosed
	}

	if s.controlStream == nil {
		return 0, nil, errors.New("aoni/masque: control stream not configured")
	}

	// Read first byte to determine varint length of Capsule Type (RFC 9297 §3.2)
	var firstByte [1]byte
	if _, err := io.ReadFull(s.controlStream, firstByte[:]); err != nil {
		return 0, nil, err
	}

	tag := firstByte[0] >> 6
	varintLen := 1 << tag

	var typeBuf [8]byte

	typeBuf[0] = firstByte[0]
	if varintLen > 1 {
		if _, err := io.ReadFull(s.controlStream, typeBuf[1:varintLen]); err != nil {
			return 0, nil, err
		}
	}

	capsuleType, _, err := DecodeVarint(typeBuf[:varintLen])
	if err != nil {
		return 0, nil, err
	}

	// Read first byte of Payload Length (RFC 9297 §3.2)
	if _, err := io.ReadFull(s.controlStream, firstByte[:]); err != nil {
		return 0, nil, err
	}

	tag = firstByte[0] >> 6
	varintLen = 1 << tag

	var lenBuf [8]byte

	lenBuf[0] = firstByte[0]
	if varintLen > 1 {
		if _, err := io.ReadFull(s.controlStream, lenBuf[1:varintLen]); err != nil {
			return 0, nil, err
		}
	}

	payloadLen, _, err := DecodeVarint(lenBuf[:varintLen])
	if err != nil {
		return 0, nil, err
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(s.controlStream, payload); err != nil {
			return 0, nil, err
		}
	}

	return capsuleType, payload, nil
}

// Close gracefully closes the control stream and marks the session as closed.
func (s *Session) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		if s.controlStream != nil {
			return s.controlStream.Close()
		}
	}

	return nil
}

// DialH3IPProxy opens an H3 Extended CONNECT IP proxying session over an active QUIC connection
// per RFC 9484 Section 4.4 & Section 4.5 and RFC 9220 Section 3 (:protocol = connect-ip).
// If reqAddrs is non-empty, an ADDRESS_REQUEST capsule (RFC 9484 §4.7.2) is sent to negotiate IP allocation.
func DialH3IPProxy(
	ctx context.Context,
	qConn *quic.Conn,
	_ string,
	reqAddrs []RequestedAddress,
) (*Session, []AssignedAddress, error) {
	if qConn == nil {
		return nil, nil, errors.New("aoni/masque: nil quic connection")
	}

	str, err := qConn.OpenStreamSync(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("aoni/masque: open connect-ip stream: %w", err)
	}

	sess := NewSession(str, qConn)

	if len(reqAddrs) > 0 {
		var reqPayloadBuf [1024]byte

		n, err := EncodeAddressRequestPayload(reqAddrs, reqPayloadBuf[:])
		if err != nil {
			_ = sess.Close()
			return nil, nil, fmt.Errorf("aoni/masque: encode address request: %w", err)
		}

		if err := sess.WriteCapsule(CapsuleAddressRequest, reqPayloadBuf[:n]); err != nil {
			_ = sess.Close()
			return nil, nil, fmt.Errorf("aoni/masque: write address request capsule: %w", err)
		}
	}

	// Read initial response capsule (ADDRESS_ASSIGN)
	cType, payload, err := sess.ReadCapsule()
	if err != nil {
		_ = sess.Close()
		return nil, nil, fmt.Errorf("aoni/masque: read address assign capsule: %w", err)
	}

	if cType != CapsuleAddressAssign {
		_ = sess.Close()
		return nil, nil, fmt.Errorf("%w: expected ADDRESS_ASSIGN (0x01), got 0x%02x", ErrInvalidCapsule, cType)
	}

	assigned, err := DecodeAddressAssignPayload(payload)
	if err != nil {
		_ = sess.Close()
		return nil, nil, fmt.Errorf("aoni/masque: decode address assign: %w", err)
	}

	return sess, assigned, nil
}
