// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"encoding/binary"
	"unicode/utf8"

	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

// MaxStreamsLimit defines the maximum permitted value for WT_MAX_STREAMS (2^60, draft-16 §5.6.2).
const MaxStreamsLimit uint64 = 1 << 60

// CloseSessionPayload represents the application closure information in a CLOSE_WEBTRANSPORT_SESSION capsule
// per draft-ietf-webtrans-http3-16 Section 6.
type CloseSessionPayload struct {
	ApplicationErrorCode uint32
	ErrorMessage         string
}

// MaxStreamsPayload represents the stream limit in WT_MAX_STREAMS or WT_STREAMS_BLOCKED capsules (draft-16 §5.6.2 & §5.6.3).
type MaxStreamsPayload struct {
	MaxStreams uint64
}

// MaxDataPayload represents the session data limit in WT_MAX_DATA or WT_DATA_BLOCKED capsules (draft-16 §5.6.4 & §5.6.5).
type MaxDataPayload struct {
	MaxData uint64
}

// EncodeVarint encodes v into b using QUIC variable-length integer encoding (RFC 9000 §16).
func EncodeVarint(v uint64, b []byte) int {
	b1 := quicvarint.Append(b[:0], v)
	return len(b1)
}

// DecodeVarint decodes a QUIC variable-length integer from b, returning value and byte length (RFC 9000 §16).
func DecodeVarint(b []byte) (uint64, int, error) {
	v, n, err := quicvarint.Parse(b)
	if err != nil {
		return 0, 0, ErrInvalidCapsule
	}

	return v, n, nil
}

// EncodeCapsuleHeader writes capsule type and payload length varints into b per RFC 9297 Section 3.2.
func EncodeCapsuleHeader(capsuleType, payloadLen uint64, b []byte) int {
	b1 := quicvarint.Append(b[:0], capsuleType)
	n1 := len(b1)
	b2 := quicvarint.Append(b[n1:n1], payloadLen)
	n2 := len(b2)

	return n1 + n2
}

// EncodeCapsule writes complete capsule frame (type, length, payload) into dst per RFC 9297 Section 3.2.
func EncodeCapsule(capsuleType uint64, payload, dst []byte) int {
	hdrLen := EncodeCapsuleHeader(capsuleType, uint64(len(payload)), dst)
	copy(dst[hdrLen:], payload)

	return hdrLen + len(payload)
}

// TruncateErrorMessage truncates msg to at most 1024 bytes on a valid UTF-8 character boundary (draft-16 §6).
func TruncateErrorMessage(msg string) string {
	if len(msg) <= 1024 {
		return msg
	}

	b := []byte(msg[:1024])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}

	return string(b)
}

// EncodeCloseSessionPayload writes CloseSessionPayload bytes into dst per draft-ietf-webtrans-http3-16 Section 6.
func EncodeCloseSessionPayload(payload CloseSessionPayload, dst []byte) int {
	msg := TruncateErrorMessage(payload.ErrorMessage)

	binary.BigEndian.PutUint32(dst[:4], payload.ApplicationErrorCode)
	copy(dst[4:], msg)

	return 4 + len(msg)
}

// EncodeCloseSessionCapsule writes the full CLOSE_WEBTRANSPORT_SESSION capsule into dst (0 allocs).
func EncodeCloseSessionCapsule(payload CloseSessionPayload, dst []byte) int {
	msg := TruncateErrorMessage(payload.ErrorMessage)
	payloadLen := 4 + len(msg)
	hdrLen := EncodeCapsuleHeader(CapsuleCloseWebTransportSession, uint64(payloadLen), dst)

	binary.BigEndian.PutUint32(dst[hdrLen:hdrLen+4], payload.ApplicationErrorCode)
	copy(dst[hdrLen+4:], msg)

	return hdrLen + payloadLen
}

// DecodeCloseSessionPayload parses CloseSessionPayload from raw capsule payload bytes.
func DecodeCloseSessionPayload(payload []byte) (CloseSessionPayload, error) {
	var res CloseSessionPayload

	err := DecodeCloseSessionPayloadTo(payload, &res)

	return res, err
}

// DecodeCloseSessionPayloadTo parses CloseSessionPayload into pre-allocated pointer with 0 allocs.
// Validates that ErrorMessage does not exceed 1024 bytes and is valid UTF-8 (draft-16 §6).
func DecodeCloseSessionPayloadTo(payload []byte, dst *CloseSessionPayload) error {
	if len(payload) < 4 {
		return ErrInvalidCapsule
	}

	msgBytes := payload[4:]
	if len(msgBytes) > 1024 || !utf8.Valid(msgBytes) {
		return ErrInvalidCapsule
	}

	dst.ApplicationErrorCode = binary.BigEndian.Uint32(payload[:4])
	dst.ErrorMessage = string(msgBytes)

	return nil
}

// EncodeDrainSessionCapsule writes the full DRAIN_WEBTRANSPORT_SESSION capsule into dst (0 allocs)
// per draft-ietf-webtrans-http3-16 Section 4.7.
func EncodeDrainSessionCapsule(dst []byte) int {
	return EncodeCapsuleHeader(CapsuleDrainWebTransportSession, 0, dst)
}

// EncodeMaxStreamsCapsule writes a WT_MAX_STREAMS or WT_STREAMS_BLOCKED capsule into dst (0 allocs).
func EncodeMaxStreamsCapsule(capsuleType, maxStreams uint64, dst []byte) (int, error) {
	if maxStreams > MaxStreamsLimit {
		return 0, ErrFlowControl
	}

	var valBuf [8]byte

	valLen := quicvarint.Append(valBuf[:0], maxStreams)
	hdrLen := EncodeCapsuleHeader(capsuleType, uint64(len(valLen)), dst)
	copy(dst[hdrLen:], valLen)

	return hdrLen + len(valLen), nil
}

// DecodeMaxStreamsPayload decodes MaxStreamsPayload from raw capsule payload bytes (draft-16 §5.6.2 & §5.6.3).
func DecodeMaxStreamsPayload(payload []byte) (MaxStreamsPayload, error) {
	v, _, err := DecodeVarint(payload)
	if err != nil {
		return MaxStreamsPayload{}, err
	}

	if v > MaxStreamsLimit {
		return MaxStreamsPayload{}, ErrFlowControl
	}

	return MaxStreamsPayload{MaxStreams: v}, nil
}

// EncodeMaxDataCapsule writes a WT_MAX_DATA or WT_DATA_BLOCKED capsule into dst (0 allocs).
func EncodeMaxDataCapsule(capsuleType, maxData uint64, dst []byte) int {
	var valBuf [8]byte

	valLen := quicvarint.Append(valBuf[:0], maxData)
	hdrLen := EncodeCapsuleHeader(capsuleType, uint64(len(valLen)), dst)
	copy(dst[hdrLen:], valLen)

	return hdrLen + len(valLen)
}

// DecodeMaxDataPayload decodes MaxDataPayload from raw capsule payload bytes (draft-16 §5.6.4 & §5.6.5).
func DecodeMaxDataPayload(payload []byte) (MaxDataPayload, error) {
	v, _, err := DecodeVarint(payload)
	if err != nil {
		return MaxDataPayload{}, err
	}

	return MaxDataPayload{MaxData: v}, nil
}
