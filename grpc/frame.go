// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/gzip"
	"github.com/lemon4ksan/foundation/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/transport"
)

var (
	defaultFramer  = transport.NewLengthPrefixedFramer(0)
	emptyGRPCFrame = []byte{0, 0, 0, 0, 0}
	gzipWriterPool = generic.NewPool(func() *gzip.Writer {
		return gzip.NewWriter(io.Discard)
	})
	gzipReaderPool = generic.NewPool(func() *gzip.Reader {
		return &gzip.Reader{}
	})
)

// MarshalFrame encodes a Protobuf message into a 5-byte Length-Prefixed-Message per PROTOCOL-HTTP2.md.
func MarshalFrame(msg proto.Message, compressed bool) ([]byte, error) {
	if msg == nil {
		return emptyGRPCFrame, nil
	}

	if !compressed {
		size := proto.Size(msg)
		frame := make([]byte, 5, 5+size)
		frame[0] = 0 // uncompressed flag

		binary.BigEndian.PutUint32(frame[1:5], uint32(size)) //nolint:gosec

		out, err := proto.MarshalOptions{}.MarshalAppend(frame, msg)
		if err != nil {
			return nil, fmt.Errorf("aoni/grpc: failed to marshal message: %w", err)
		}

		return out, nil
	}

	rawBytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("aoni/grpc: failed to marshal message: %w", err)
	}

	var buf bytes.Buffer

	gzWriter := gzipWriterPool.Get()
	if gzWriter == nil {
		gzWriter = gzip.NewWriter(io.Discard)
	}

	gzWriter.Reset(&buf)

	if _, err := gzWriter.Write(rawBytes); err != nil {
		gzWriter.Reset(io.Discard)
		gzipWriterPool.Put(gzWriter)

		return nil, err
	}

	if err := gzWriter.Close(); err != nil {
		gzWriter.Reset(io.Discard)
		gzipWriterPool.Put(gzWriter)

		return nil, err
	}

	gzWriter.Reset(io.Discard)
	gzipWriterPool.Put(gzWriter)

	gzPayload := buf.Bytes()
	frame := make([]byte, 5+len(gzPayload))
	frame[0] = 1
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(gzPayload))) //nolint:gosec
	copy(frame[5:], gzPayload)

	return frame, nil
}

// UnmarshalFrame decodes a 5-byte Length-Prefixed-Message into a Protobuf target message.
func UnmarshalFrame(r io.Reader, target proto.Message) (bool, error) {
	flags, payload, err := defaultFramer.ReadFrame(r)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, err
		}

		return false, fmt.Errorf("%w: %w", ErrInvalidGRPCFrame, err)
	}

	compressed := flags&1 != 0
	if compressed {
		var (
			gzReader *gzip.Reader
			gzErr    error
		)

		if gzReader = gzipReaderPool.Get(); gzReader != nil {
			gzErr = gzReader.Reset(bytes.NewReader(payload))
		} else {
			gzReader, gzErr = gzip.NewReader(bytes.NewReader(payload))
		}

		if gzErr != nil {
			return true, fmt.Errorf("aoni/grpc: failed to decompress payload: %w", gzErr)
		}

		decompressed, readErr := io.ReadAll(gzReader)
		_ = gzReader.Close()
		gzipReaderPool.Put(gzReader)

		if readErr != nil {
			return true, fmt.Errorf("aoni/grpc: failed to read decompressed payload: %w", readErr)
		}

		payload = decompressed
	}

	if err := proto.Unmarshal(payload, target); err != nil {
		return compressed, err
	}

	return compressed, nil
}
