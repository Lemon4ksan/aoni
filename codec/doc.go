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
//     are supported via [DecodeTo] and [DecodeToResult].
//  3. High-Performance URL Query Reflection: [Encode] and [StructToQueryString] serialize nested
//     structs and maps into RFC 3986 query parameters using cached struct schemas ([values.Encode]).
//  4. 5-Byte Framed gRPC-Web Decoding: Conforms to the gRPC-Web specification with length-prefixed
//     framing, payload decompression, and trailer status validation.
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
//	res := codec.DecodeToResult[User](resp.Body, codec.JSONDecoder)
//	if user, ok := res.Value(); ok {
//	    fmt.Printf("User: %s\n", user.Name)
//	}
//
//	// Or seamlessly wrap any (T, error) via foundation/generic:
//	res = generic.FromResult(codec.JSON[User](resp.Body))
package codec
