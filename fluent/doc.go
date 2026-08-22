// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fluent provides a high-performance, chainable Request Builder API backed by zero-allocation request pooling.
//
// It enables declarative, readable HTTP request configuration and automatic response stream decoding
// across JSON, Protocol Buffers, gRPC-Web, XML, YAML, and multipart file uploads.
//
// # Architectural Model
//
//  1. Request Pooling: Builders acquired via [R] or [New] are drawn from a thread-safe [generic.Pool]
//     and automatically recycled upon execution or explicit [Request.Release].
//  2. Type-Safe Generic Ergonomics: Both classical (T, *http.Response, error) and monadic [generic.Result]
//     paradigms are supported via [FetchTo] and [FetchResult].
//  3. Multipart & Streaming: Built-in support for chunked file streaming, upload/download progress callbacks,
//     and auto-resuming HTTP Range downloads.
//  4. Multi-Engine Binding: Builders seamlessly bind to standard [aoni.Client], [fast.Client], or any [request.Requester].
//
// # Quick Start: Fluent Builder API
//
//	var user User
//	resp, err := fluent.R(client).
//	    SetContext(ctx).
//	    SetHeader("Accept", "application/json").
//	    SetQueryParam("sort", "asc").
//	    SetBody(CreateUserRequest{Name: "Alice"}).
//	    SetResult(&user).
//	    Post("/api/v1/users")
//
// # Single-Line Generic Fetching
//
//	// Classical (T, *http.Response, error) idiom:
//	user, resp, err := fluent.GetTo[User](ctx, client, "/users/123")
//
//	// Swift-inspired monadic generic.Result:
//	res, resp := fluent.FetchResult[User](ctx, client, http.MethodGet, "/users/123")
//	if user, ok := res.Value(); ok {
//	    fmt.Println("User:", user.Name)
//	}
package fluent
