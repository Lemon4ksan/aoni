// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license Image by BSD-style license.

// Package values provides custom serialization types and structured parameter encoding.
//
// It bridges Go's strongly-typed structures with standard web parameter schemas (such as
// URL query parameters and URL-encoded form payloads), supporting advanced features like custom
// slice delimiters, inline nesting, and automatic JSON fallback serialization.
//
// # Serialization & Custom Types
//
// The package includes several built-in custom unmarshalers to safely parse non-standard API
// data types from JSON payloads:
//   - [BoolInt]: Safely parses booleans represented as numbers (e.g. 1 or 0) or standard strings.
//   - [Uint64String], [Int64String], [Float64String]: Parse numbers transmitted inside string quotes.
//   - [UnixTimestamp]: Parses standard UNIX epoch timestamps directly into standard [time.Time] structs.
//   - [RFC3339Timestamp]: Parses ISO-8601 and RFC-3339 formatted date-time strings.
//
// # Advanced Query & Form Encoding
//
// [StructToValues] is the central marshaller of the package. It reflects Go structs into standard
// [url.Values] using "url" or "json" tags, resolving pointers and anonymous structures recursively.
// It includes several major design extensions to support diverse API schemas:
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
//     it is converted directly using its MarshalText method (resulting in clean, compliant strings
//     like RFC-3339 timestamps) instead of raising reflection errors.
//
//  3. Automatic JSON Fallback:
//     If a field is a map or non-inlined nested structure that does not implement standard string conversion,
//     it is automatically serialized into a JSON string (e.g. ?meta={"key":"value"}).
//
// # Example
//
//	package main
//
//	import (
//		"fmt"
//		"net/url"
//		"time"
//		"github.com/lemon4ksan/aoni/values"
//	)
//
//	type Metadata struct {
//		System string `json:"sys"`
//	}
//
//	type RequestParams struct {
//		Name      string                  `url:"name"`
//		IsActive  values.BoolInt          `url:"active"`
//		Tags      []string                `url:"tags,comma"`
//		Meta      Metadata                `url:"meta"`
//		Created   values.RFC3339Timestamp `url:"created"`
//	}
//
//	func main() {
//		p := RequestParams{
//			Name:     "administrator",
//			IsActive: values.BoolInt(true),
//			Tags:     []string{"admin", "internal"},
//			Meta:     Metadata{System: "production"},
//			Created:  values.RFC3339Timestamp(time.Now()),
//		}
//
//		vals, err := values.StructToValues(p)
//		if err != nil {
//			panic(err)
//		}
//
//		// Prints: active=1&created=2026-07-21T21%3A46%3A00Z&meta=%7B%22sys%22%3A%22production%22%7D&name=administrator&tags=admin%2Cinternal
//		fmt.Println(vals.Encode())
//	}
package values
