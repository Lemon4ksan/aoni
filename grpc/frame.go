// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"google.golang.org/protobuf/proto"
)

var headerBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 5)
		return &b
	},
}

// MarshalFrame encodes a Protobuf message into a 5-byte Length-Prefixed-Message per PROTOCOL-HTTP2.md.
func MarshalFrame(msg proto.Message, compressed bool) ([]byte, error) {
	if msg == nil {
		return []byte{0, 0, 0, 0, 0}, nil
	}

	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("aoni grpc: marshal proto failed: %w", err)
	}

	payloadLen := len(payload)

	frame := make([]byte, 5+payloadLen)
	if compressed {
		frame[0] = 0x01
	} else {
		frame[0] = 0x00
	}

	binary.BigEndian.PutUint32(frame[1:5], uint32(payloadLen)) //nolint:gosec
	copy(frame[5:], payload)

	return frame, nil
}

// UnmarshalFrame reads a Length-Prefixed-Message from reader and decodes the Protobuf payload.
func UnmarshalFrame(r io.Reader, target proto.Message) (compressed bool, err error) {
	hdrPtr := headerBufferPool.Get().(*[]byte)
	defer headerBufferPool.Put(hdrPtr)

	hdr := *hdrPtr
	if _, err := io.ReadFull(r, hdr); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidGRPCFrame, err)
	}

	compressed = hdr[0] != 0x00
	length := binary.BigEndian.Uint32(hdr[1:5])

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidGRPCFrame, err)
	}

	if err := proto.Unmarshal(payload, target); err != nil {
		return false, fmt.Errorf("aoni grpc: unmarshal proto failed: %w", err)
	}

	return compressed, nil
}
