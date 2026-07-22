// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package values provides custom serialization types and structured parameter encoding.
//
// It bridges Go's strongly-typed structures and Protocol Buffer messages with standard web parameter schemas
// (such as URL query parameters and URL-encoded form payloads), supporting advanced features like custom
// slice delimiters, inline nesting, Protocol Buffers mapping, and automatic JSON fallback serialization.
//
// # Serialization & Custom Types
//
// The package includes several built-in custom unmarshalers to safely parse non-standard API
// data types from JSON payloads and encode slice parameters cleanly:
//   - [CommaSlice]: Generic slice type (e.g. CommaSlice[int]) that automatically formats items as comma-separated values.
//   - [BoolInt]: Safely parses booleans represented as numbers (e.g. 1 or 0) or standard strings.
//   - [Uint64String], [Int64String], [Float64String]: Parse numbers transmitted inside string quotes.
//   - [UnixTimestamp]: Parses standard UNIX epoch timestamps directly into standard [time.Time] structs.
//   - [RFC3339Timestamp]: Parses ISO-8601 and RFC-3339 formatted date-time strings.
//
// # Protocol Buffers Integration
//
// [StructToValues] natively supports [google.golang.org/protobuf/proto.Message] objects both as top-level arguments
// and as nested struct fields. Protobuf fields are serialized using [google.golang.org/protobuf/encoding/protojson]
// with snake_case field preservation (`UseProtoNames: true`).
//
// # Advanced Query & Form Encoding
//
// [StructToValues] reflects Go structs and maps into standard [url.Values] using "url" or "json" tags,
// resolving pointers and anonymous structures recursively with schema caching ([sync.Map]):
//
//  1. Custom Slice Delimiters:
//     Slices can be formatted using specific string separators on the wire.
//     Add the optional target delimiter keyword inside the struct's url tag option:
//     - `url:"tags,comma"` -> produces ?tags=go,rust,python
//     - `url:"tags,space"` -> produces ?tags=go+rust+python
//     - `url:"tags,pipe"`  -> produces ?tags=go|rust|pipe
//
//  2. TextMarshaler Interception:
//     If a nested type implements [encoding.TextMarshaler] (such as standard [time.Time] or [net.IP]),
//     it is converted directly using its MarshalText method instead of raising reflection errors.
//
//  3. Automatic JSON Fallback:
//     If a field is a map or non-inlined nested structure that does not implement standard string conversion,
//     it is automatically serialized into a JSON string (e.g. ?meta={"key":"value"}).
//
// # Error Handling
//
// All reflection and serialization routines produce structured [ValueError] instances that eliminate
// eager string formatting overhead. These errors support standard Go error unwrapping via [errors.Is] and [errors.As].
//
// # Example
//
//	package main
//
//	import (
//		"fmt"
//	"github.com/lemon4ksan/aoni/codec/values"
//	"google.golang.org/protobuf/types/known/wrapperspb"
//
// )
//
//	type RequestParams struct {
//		Name     string                    `url:"name"`
//		IsActive values.BoolInt            `url:"active"`
//		Tags     values.CommaSlice[string] `url:"tags"`
//		Meta     *wrapperspb.StringValue   `url:"meta"`
//	}
//
//	func main() {
//		p := RequestParams{
//			Name:     "administrator",
//			IsActive: values.BoolInt(true),
//			Tags:     values.CommaSlice[string]{"admin", "internal"},
//			Meta:     wrapperspb.String("production"),
//		}
//
//		vals, err := values.StructToValues(p)
//		if err != nil {
//			panic(err)
//		}
//
//		// Prints: active=1&meta=%7B%22value%22%3A%22production%22%7D&name=administrator&tags=admin%2Cinternal
//		fmt.Println(vals.Encode())
//	}
package values
