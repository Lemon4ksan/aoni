// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codec provides a unified, zero-allocation serialization and deserialization engine for Go.
//
// It provides a high-performance facade for serializing outbound request payloads and deserializing
// incoming response streams across JSON, Protocol Buffers, 5-byte framed gRPC-Web, XML, YAML,
// and URL query parameters.
//
// # Architectural Pillars
//
//  1. Zero-Allocation Fast Paths: Decoders detect in-memory buffers ([fio.BytesReader]) and avoid
//     intermediate copies, decoding directly from contiguous memory via SIMD and zero-copy string views.
//  2. Swift/Rust Generic Ergonomics: Both classical (T, error) and monadic [generic.Result] paradigms
//     are supported via [To] and [Result].
//  3. High-Performance URL Query Reflection: [Encode] and [StructToQueryString] serialize nested
//     structs and maps into RFC 3986 query parameters using cached struct schemas ([values.Encode]).
//  4. 5-Byte Framed gRPC-Web Decoding: Conforms to the gRPC-Web specification with length-prefixed
//     framing, payload decompression, and trailer status validation.
//
// # Primary Facade API
//
//	| Function                | Description                                                              |
//	|-------------------------|--------------------------------------------------------------------------|
//	| [To]                    | Decode response stream into T using any [Decoder] strategy                |
//	| [Result]                | Decode response stream into [generic.Result] using any [Decoder] strategy |
//	| [JSON]                  | Stream decode standard JSON into a newly allocated T                     |
//	| [Proto]                 | Stream decode binary Protocol Buffer into [proto.Message] T              |
//	| [GRPCWeb]               | Stream decode 5-byte framed gRPC-Web payload and validate trailers       |
//	| [XML]                   | Stream decode XML into a newly allocated T                               |
//	| [YAML]                  | Stream decode YAML into a newly allocated T                              |
//	| [Raw]                   | Stream read entire response body directly into []byte                     |
//	| [Payload]               | Auto-match MIME Content-Type and decode in-memory byte slices             |
//	| [Encode]                | Reflect struct/map fields into [url.Values]                              |
//	| [StructToQueryString]   | Serialize struct/map into an RFC 3986 URL query string                   |
//
// # Quick Start: Decoding Responses
//
//	// Standard (T, error) idiom:
//	user, err := codec.JSON[User](resp.Body)
//	if err != nil {
//	    return err
//	}
//
//	// Functional generic.Result idiom:
//	res := codec.Result[User](resp.Body, codec.JSONDecoder)
//	if user, ok := res.Value(); ok {
//	    fmt.Printf("User: %s\n", user.Name)
//	}
//
//	// Or seamlessly wrap any (T, error) via foundation/generic:
//	res = generic.FromResult(codec.JSON[User](resp.Body))
package codec
