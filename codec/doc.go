// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codec defines high-level request body encoding and response body decoding contracts.
//
// It provides a unified, zero-allocation facade for serializing outbound request payloads
// and deserializing incoming response streams across JSON, Protocol Buffers, gRPC-Web, XML,
// and URL query values.
//
// # Unified Facade
//
// The package re-exports primary decoding and encoding facilities:
//   - [Decoder]: Primary interface for response body stream parsers.
//   - [JSONCodec], [ProtoCodec], [GRPCWebCodec]: Pre-packaged [Codec] strategies matching content types to decoders.
//   - [Encode], [StructToQueryString]: High-performance struct reflection to [url.Values] query parameters.
//
// Subpackages [github.com/lemon4ksan/aoni/codec/decode] and [github.com/lemon4ksan/aoni/codec/values]
// remain available for fine-grained specialized type access.
package codec
