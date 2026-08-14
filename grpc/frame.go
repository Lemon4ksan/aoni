// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/gzip"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/transport"
)

var (
	defaultFramer  = transport.NewLengthPrefixedFramer(0)
	emptyGRPCFrame = []byte{0, 0, 0, 0, 0}
)

// marshalFrame encodes a Protobuf message into a 5-byte Length-Prefixed-Message per PROTOCOL-HTTP2.md.
func marshalFrame(msg proto.Message, compressed bool) ([]byte, error) {
	if msg == nil {
		return emptyGRPCFrame, nil
	}

	rawBytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("aoni/grpc: failed to marshal message: %w", err)
	}

	var flags byte
	if compressed {
		flags = 1

		var buf bytes.Buffer

		gzWriter := gzip.NewWriter(&buf)

		if _, err := gzWriter.Write(rawBytes); err != nil {
			return nil, err
		}

		_ = gzWriter.Close()
		rawBytes = buf.Bytes()
	}

	var buf bytes.Buffer
	if _, err := defaultFramer.WriteFrame(&buf, flags, rawBytes); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// unmarshalFrame decodes a 5-byte Length-Prefixed-Message into a Protobuf target message.
func unmarshalFrame(r io.Reader, target proto.Message) (bool, error) {
	flags, payload, err := defaultFramer.ReadFrame(r)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, err
		}

		return false, fmt.Errorf("%w: %w", ErrInvalidGRPCFrame, err)
	}

	compressed := flags&1 != 0
	if compressed {
		gzReader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return true, fmt.Errorf("aoni/grpc: failed to decompress payload: %w", err)
		}

		payload, err = io.ReadAll(gzReader)
		_ = gzReader.Close()

		if err != nil {
			return true, fmt.Errorf("aoni/grpc: failed to read decompressed payload: %w", err)
		}
	}

	if err := proto.Unmarshal(payload, target); err != nil {
		return compressed, err
	}

	return compressed, nil
}
