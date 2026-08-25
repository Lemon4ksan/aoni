// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webtransport

import (
	"errors"
	"strings"
	"testing"
)

func TestEncodeDecodeCloseSessionCapsule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload CloseSessionPayload
	}{
		{
			name: "zero_code_empty_msg",
			payload: CloseSessionPayload{
				ApplicationErrorCode: 0,
				ErrorMessage:         "",
			},
		},
		{
			name: "application_error_with_message",
			payload: CloseSessionPayload{
				ApplicationErrorCode: 404,
				ErrorMessage:         "resource not found in session",
			},
		},
		{
			name: "max_code_long_message",
			payload: CloseSessionPayload{
				ApplicationErrorCode: 0xffffffff,
				ErrorMessage:         "critical internal session crash during live video feed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf [512]byte

			n := EncodeCloseSessionCapsule(tt.payload, buf[:])
			if n <= 0 {
				t.Fatalf("expected positive capsule length, got %d", n)
			}

			// Parse header
			cType, offset, err := DecodeVarint(buf[:n])
			if err != nil {
				t.Fatalf("decode capsule type failed: %v", err)
			}

			if cType != CapsuleCloseWebTransportSession {
				t.Fatalf("expected capsule type 0x%x, got 0x%x", CapsuleCloseWebTransportSession, cType)
			}

			payloadLen, lenN, err := DecodeVarint(buf[offset:n])
			if err != nil {
				t.Fatalf("decode payload length failed: %v", err)
			}

			payloadOffset := offset + lenN
			if uint64(len(buf[payloadOffset:n])) != payloadLen {
				t.Fatalf("expected payload len %d, got %d", payloadLen, len(buf[payloadOffset:n]))
			}

			// Decode payload
			decoded, err := DecodeCloseSessionPayload(buf[payloadOffset:n])
			if err != nil {
				t.Fatalf("decode payload failed: %v", err)
			}

			if decoded.ApplicationErrorCode != tt.payload.ApplicationErrorCode {
				t.Errorf(
					"expected error code %d, got %d",
					tt.payload.ApplicationErrorCode,
					decoded.ApplicationErrorCode,
				)
			}

			if decoded.ErrorMessage != tt.payload.ErrorMessage {
				t.Errorf("expected message %q, got %q", tt.payload.ErrorMessage, decoded.ErrorMessage)
			}

			// Zero-alloc decode into target pointer
			var target CloseSessionPayload
			if err := DecodeCloseSessionPayloadTo(buf[payloadOffset:n], &target); err != nil {
				t.Fatalf("DecodeCloseSessionPayloadTo failed: %v", err)
			}

			if target != tt.payload {
				t.Errorf("DecodeCloseSessionPayloadTo mismatched: %+v != %+v", target, tt.payload)
			}
		})
	}
}

func TestEncodeDrainSessionCapsule(t *testing.T) {
	t.Parallel()

	var buf [32]byte

	n := EncodeDrainSessionCapsule(buf[:])
	if n <= 0 {
		t.Fatalf("expected positive length, got %d", n)
	}

	cType, offset, err := DecodeVarint(buf[:n])
	if err != nil {
		t.Fatalf("decode varint failed: %v", err)
	}

	if cType != CapsuleDrainWebTransportSession {
		t.Fatalf("expected capsule 0x%x, got 0x%x", CapsuleDrainWebTransportSession, cType)
	}

	payloadLen, _, err := DecodeVarint(buf[offset:n])
	if err != nil {
		t.Fatalf("decode length failed: %v", err)
	}

	if payloadLen != 0 {
		t.Fatalf("expected payload len 0 for DRAIN capsule, got %d", payloadLen)
	}
}

func TestDecodeCloseSessionPayload_Errors(t *testing.T) {
	t.Parallel()

	var target CloseSessionPayload

	// Truncated payload (< 4 bytes)
	err := DecodeCloseSessionPayloadTo([]byte{0x01, 0x02}, &target)
	if !errors.Is(err, ErrInvalidCapsule) {
		t.Errorf("expected ErrInvalidCapsule on truncated payload, got %v", err)
	}

	// Empty payload
	err = DecodeCloseSessionPayloadTo(nil, &target)
	if !errors.Is(err, ErrInvalidCapsule) {
		t.Errorf("expected ErrInvalidCapsule on empty payload, got %v", err)
	}

	// Payload message > 1024 bytes (draft-16 §6)
	overlong := make([]byte, 4+1025)
	for i := range overlong[4:] {
		overlong[4+i] = 'a'
	}

	err = DecodeCloseSessionPayloadTo(overlong, &target)
	if !errors.Is(err, ErrInvalidCapsule) {
		t.Errorf("expected ErrInvalidCapsule on >1024 byte payload, got %v", err)
	}

	// Payload message invalid UTF-8 (draft-16 §6)
	invalidUTF8 := []byte{0x00, 0x00, 0x00, 0x01, 0xff, 0xfe, 0xfd}

	err = DecodeCloseSessionPayloadTo(invalidUTF8, &target)
	if !errors.Is(err, ErrInvalidCapsule) {
		t.Errorf("expected ErrInvalidCapsule on invalid UTF-8, got %v", err)
	}
}

func TestTruncateErrorMessage(t *testing.T) {
	t.Parallel()

	shortMsg := "hello world"
	if TruncateErrorMessage(shortMsg) != shortMsg {
		t.Errorf("expected unchanged short message, got %q", TruncateErrorMessage(shortMsg))
	}

	// Test 1100 character ASCII string
	longMsg := strings.Repeat("x", 1100)

	truncated := TruncateErrorMessage(longMsg)
	if len(truncated) != 1024 {
		t.Errorf("expected length 1024, got %d", len(truncated))
	}
}

func TestFlowControlCapsules_EncodeDecode(t *testing.T) {
	t.Parallel()

	var buf [64]byte

	// 1. WT_MAX_STREAMS Bidi
	n, err := EncodeMaxStreamsCapsule(CapsuleWTMaxStreamsBidi, 100, buf[:])
	if err != nil {
		t.Fatalf("EncodeMaxStreamsCapsule failed: %v", err)
	}

	cType, offset, err := DecodeVarint(buf[:n])
	if err != nil || cType != CapsuleWTMaxStreamsBidi {
		t.Fatalf("expected CapsuleWTMaxStreamsBidi (0x%x), got 0x%x", CapsuleWTMaxStreamsBidi, cType)
	}

	payloadLen, lenN, err := DecodeVarint(buf[offset:n])
	if err != nil {
		t.Fatalf("decode payload len failed: %v", err)
	}

	pOffset := offset + lenN

	ms, err := DecodeMaxStreamsPayload(buf[pOffset : pOffset+int(payloadLen)])
	if err != nil {
		t.Fatalf("DecodeMaxStreamsPayload failed: %v", err)
	}

	if ms.MaxStreams != 100 {
		t.Errorf("expected MaxStreams 100, got %d", ms.MaxStreams)
	}

	// 2. WT_MAX_DATA
	n = EncodeMaxDataCapsule(CapsuleWTMaxData, 1048576, buf[:])

	cType, offset, err = DecodeVarint(buf[:n])
	if err != nil || cType != CapsuleWTMaxData {
		t.Fatalf("expected CapsuleWTMaxData (0x%x), got 0x%x", CapsuleWTMaxData, cType)
	}

	payloadLen, lenN, err = DecodeVarint(buf[offset:n])
	if err != nil {
		t.Fatalf("decode payload len failed: %v", err)
	}

	pOffset = offset + lenN

	md, err := DecodeMaxDataPayload(buf[pOffset : pOffset+int(payloadLen)])
	if err != nil {
		t.Fatalf("DecodeMaxDataPayload failed: %v", err)
	}

	if md.MaxData != 1048576 {
		t.Errorf("expected MaxData 1048576, got %d", md.MaxData)
	}

	// 3. Exceeds MaxStreamsLimit (2^60)
	_, err = EncodeMaxStreamsCapsule(CapsuleWTMaxStreamsUni, MaxStreamsLimit+1, buf[:])
	if !errors.Is(err, ErrFlowControl) {
		t.Errorf("expected ErrFlowControl on exceeding MaxStreamsLimit, got %v", err)
	}
}
