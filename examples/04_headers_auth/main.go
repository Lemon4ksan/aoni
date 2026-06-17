// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Bearer token, Basic auth, and custom headers.
//
// Demonstrates per-request authentication and header modifiers.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

type ProtectedResource struct {
	Message string `json:"message"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org")

	// Bearer token authentication
	res, err := aoni.GetJSON[ProtectedResource](ctx, client,
		"/bearer",
		aoni.WithBearer("my-secret-token"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Bearer:", res.Message)

	// Basic authentication
	basicRes, err := aoni.GetJSON[ProtectedResource](ctx, client,
		"/basic-auth/user/passwd",
		aoni.WithBasicAuth("user", "passwd"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Basic:", basicRes.Message)

	// Custom headers
	headerRes, err := aoni.GetJSON[ProtectedResource](ctx, client,
		"/headers",
		aoni.WithHeader("X-Custom-Header", "hello-aoni"),
		aoni.WithHeader("X-Request-ID", "abc-123"),
		aoni.WithOrigin("https://example.com"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Custom headers:", headerRes.Message)
}
