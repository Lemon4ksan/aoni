// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	stdio "io"

	"github.com/klauspost/compress/gzip"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/transport"
)

type grpcWebDecoder struct{}

func (grpcWebDecoder) Decode(r stdio.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	br := bufio.NewReader(r)

	var reader stdio.Reader = br
	if peek, err := br.Peek(5); err == nil && IsBase64Header(peek) {
		reader = base64.NewDecoder(base64.StdEncoding, br)
	}

	return readGRPCWebFrames(reader, msg)
}

// readGRPCWebFrames sequentially reads 5-byte length-prefixed frames from reader and unmarshals payload data into msg.
func readGRPCWebFrames(reader stdio.Reader, msg proto.Message) error {
	var payloadRead bool

	framer := transport.NewLengthPrefixedFramer(0)
	for {
		flags, payload, err := framer.ReadFrame(reader)
		if err != nil {
			if errors.Is(err, stdio.EOF) {
				return nil
			}

			if errors.Is(err, transport.ErrTruncatedPayload) && payloadRead {
				return nil
			}

			op := "read_header"
			if errors.Is(err, transport.ErrTruncatedPayload) {
				op = "read_payload"
			}

			return &GRPCWebError{Op: op, Err: ErrInvalidGRPCWebFrame}
		}

		done, err := processGRPCWebFrame(flags, payload, msg)
		if err != nil {
			return err
		}

		if done {
			return nil
		}

		payloadRead = true
	}
}

// processGRPCWebFrame handles frame flags (compression, trailer marker) and unmarshals payload data into msg.
func processGRPCWebFrame(flags byte, payload []byte, msg proto.Message) (done bool, err error) {
	if flags&0x80 != 0 {
		if err := verifyGRPCTrailer(payload); err != nil {
			return false, err
		}

		return true, nil
	}

	if flags&0x01 != 0 {
		var err error

		payload, err = decompressProtoPayload(payload)
		if err != nil {
			return false, err
		}
	}

	if err := proto.Unmarshal(payload, msg); err != nil {
		return false, &Error{Format: "grpc-web", Target: typeName(msg), Err: err}
	}

	return false, nil
}

// decompressProtoPayload decompresses a gzip-encoded Protobuf payload stream.
func decompressProtoPayload(payload []byte) ([]byte, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, &GRPCWebError{Op: "decompress", Err: err}
	}

	decompressed, err := stdio.ReadAll(gzReader)
	_ = gzReader.Close()

	if err != nil {
		return nil, &GRPCWebError{Op: "read_decompressed", Err: err}
	}

	return decompressed, nil
}

// VerifyGRPCTrailer parses gRPC-Web trailer key-value headers and validates grpc-status codes.
func VerifyGRPCTrailer(trailerPayload []byte) error {
	return verifyGRPCTrailer(trailerPayload)
}

// verifyGRPCTrailer inspects raw trailer lines and returns a [GRPCWebError] if grpc-status indicates failure.
func verifyGRPCTrailer(trailerPayload []byte) error {
	var (
		statusCode, statusMsg string
		statusDetails         []byte
	)

	for len(trailerPayload) > 0 {
		var line []byte

		if idx := bytes.IndexByte(trailerPayload, '\n'); idx >= 0 {
			line = trailerPayload[:idx]
			trailerPayload = trailerPayload[idx+1:]
		} else {
			line = trailerPayload
			trailerPayload = nil
		}

		keyBytes, valBytes, ok := parseTrailerKeyValue(line)
		if !ok {
			continue
		}

		keyStr := bytesconv.B2S(keyBytes)

		switch {
		case bytesconv.EqualFoldASCII(keyStr, "grpc-status"):
			statusCode = bytesconv.B2S(valBytes)
		case bytesconv.EqualFoldASCII(keyStr, "grpc-message"):
			statusMsg = bytesconv.B2S(valBytes)
		case bytesconv.EqualFoldASCII(keyStr, "grpc-status-details-bin"):
			valStr := bytesconv.B2S(valBytes)
			if decoded, err := base64.RawStdEncoding.DecodeString(valStr); err == nil {
				statusDetails = decoded
			} else if decoded, err := base64.StdEncoding.DecodeString(valStr); err == nil {
				statusDetails = decoded
			}
		}
	}

	if statusCode != "" && statusCode != "0" {
		return &GRPCWebError{
			StatusCode:    statusCode,
			StatusMsg:     statusMsg,
			StatusDetails: statusDetails,
			Err:           ErrGRPCWebStatusError,
		}
	}

	return nil
}

// parseTrailerKeyValue splits a raw trailer line by ':' and trims leading/trailing whitespace without allocations.
func parseTrailerKeyValue(line []byte) (keyBytes, valBytes []byte, ok bool) {
	idx := bytes.IndexByte(line, ':')
	if idx < 0 {
		return nil, nil, false
	}

	return bytes.TrimSpace(line[:idx]), bytes.TrimSpace(line[idx+1:]), true
}

// IsBase64Header checks whether frame prefix matches Base64 text-encoded gRPC-Web stream.
func IsBase64Header(header []byte) bool {
	if len(header) < 5 {
		return false
	}

	first := header[0]

	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z') || (first >= '0' && first <= '9')
}
