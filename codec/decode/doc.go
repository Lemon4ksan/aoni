// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package decode provides type-safe, high-performance response body decoders for unmarshaling structured payload streams.
//
// It serves as the primary data extraction layer for the aoni HTTP client, supporting multiple serialization
// formats including JSON, Protocol Buffers, gRPC-Web, XML, YAML, and raw byte streams.
//
// # Supported Formats & Decoders
//
// The package offers built-in implementations of the [Decoder] interface for common wire formats:
//   - [JSONDecoder]: Standard JSON decoder with automatic BOM stripping (UTF-8, UTF-16).
//   - [ProtoDecoder]: Reads raw binary Protocol Buffer streams directly into [google.golang.org/protobuf/proto.Message] targets.
//   - [GRPCWebDecoder]: Parses gRPC-Web response streams (binary or Base64-text), handling 5-byte frame headers,
//     gzip frame decompression, and trailer status validation.
//   - [ProtoJSONDecoder]: Unmarshals JSON strings into Protobuf messages via [google.golang.org/protobuf/encoding/protojson].
//   - [XMLDecoder]: Unmarshals XML payloads into Go structures.
//   - [YAMLDecoder]: Unmarshals YAML payloads into Go structures.
//   - [RawDecoder]: Reads the entire payload stream directly into a byte slice (*[]byte) using memory pools.
//
// # Automatic Content Negotiation
//
// [ByContentType] automatically inspects the MIME-type from a response header (e.g. "application/grpc-web+proto")
// and delegates stream decoding to the appropriate registered decoder, falling back to [RawDecoder] if unrecognized.
//
// # Type-Safe Generic Helpers
//
// Convenience generic functions are provided to instantiate and decode streams into a newly allocated T in a single call:
//   - [JSON], [Proto], [GRPCWeb], [XML], [YAML], and the general-purpose [To].
//
// # Structured Error Handling
//
// All decoders emit structured error types ([Error], [GRPCWebError]) that avoid eager string allocations.
// These error structs retain dynamic context (e.g. target type names, gRPC status codes, I/O operations)
// and support standard Go error inspection via [errors.Is] and [errors.As].
//
// # Example
//
//	package main
//
//	import (
//		"bytes"
//		"fmt"
//
//		"github.com/lemon4ksan/aoni/codec/decode"
//	)
//
//	type UserProfile struct {
//		ID    int    `json:"id"`
//		Name  string `json:"name"`
//		Email string `json:"email"`
//	}
//
//	func main() {
//		stream := bytes.NewReader([]byte(`{"id":42,"name":"Alice","email":"alice@example.com"}`))
//
//		// Decode directly into a newly allocated UserProfile struct
//		user, err := decode.JSON[UserProfile](stream)
//		if err != nil {
//			panic(err)
//		}
//
//		fmt.Printf("User #%d: %s <%s>\n", user.ID, user.Name, user.Email)
//	}
package decode
