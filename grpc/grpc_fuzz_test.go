// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc_test

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/lemon4ksan/aoni/grpc"
)

// FuzzGRPCWebFraming tests 5-byte Length-Prefixed-Message unmarshaling against arbitrary byte streams.
func FuzzGRPCWebFraming(f *testing.F) {
	f.Add([]byte{0x00, 0x00, 0x00, 0x00, 0x00})                         // Empty uncompressed frame
	f.Add([]byte{0x01, 0x00, 0x00, 0x00, 0x04, 0x01, 0x02, 0x03, 0x04}) // Compressed frame
	f.Add([]byte{0x80, 0x00, 0x00, 0x00, 0x02, 0x30, 0x31})             // Trailer frame (bit 7 set)
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00}) // Truncated header

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		r := bytes.NewReader(data)
		target := &emptypb.Empty{}
		_, _ = grpc.UnmarshalFrame(r, target)
	})
}

// FuzzGRPCStatusParsing tests gRPC status code and message extraction from HTTP headers and trailers.
func FuzzGRPCStatusParsing(f *testing.F) {
	f.Add("0", "OK")
	f.Add("14", "unavailable")
	f.Add("invalid_status", "message with %20 percent encoding")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, statusCodeStr, statusMsg string) {
		header := make(http.Header)
		if statusCodeStr != "" {
			header.Set("grpc-status", statusCodeStr)
		}

		if statusMsg != "" {
			header.Set("grpc-message", statusMsg)
		}

		statusErr := grpc.ParseGRPCStatus(header)
		if statusErr != nil {
			_ = statusErr.Error()
		}
	})
}

// FuzzGRPCTimeoutFormat tests gRPC timeout header encoding resilience across negative, zero, and huge durations.
func FuzzGRPCTimeoutFormat(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(100 * time.Millisecond))
	f.Add(int64(5 * time.Second))
	f.Add(int64(10 * time.Minute))
	f.Add(int64(24 * time.Hour))
	f.Add(int64(-1))

	f.Fuzz(func(t *testing.T, dNs int64) {
		d := time.Duration(dNs)

		formatted := grpc.FormatTimeout(d)
		if d > 0 && len(formatted) == 0 {
			t.Fatalf("expected non-empty formatted timeout for %v", d)
		}
	})
}
