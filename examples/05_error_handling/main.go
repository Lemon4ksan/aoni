// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Error handling with APIError, ErrCloudflareChallenge, and status codes.
//
// Demonstrates how to handle various error types returned by the aoni client,
// including typed API errors, Cloudflare challenges, and status code checking.
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/resiliency/challenge"

	"github.com/lemon4ksan/aoni"
)

type NotFoundResponse struct {
	Error string `json:"error"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := aoni.NewClient(nil,
		option.WithBaseURL("https://jsonplaceholder.typicode.com"),
	)

	// Example 1: Check for aoni.APIError with a custom error model
	_, err := request.GetTo[any](ctx, client,
		"/posts/99999",
		mod.WithErrorModel(&NotFoundResponse{}),
	)
	if err != nil {
		var apiErr *aoni.APIError
		if errors.As(err, &apiErr) {
			fmt.Printf("API Error: status=%d body=%s\n", apiErr.StatusCode, string(apiErr.Body))

			if nf, ok := apiErr.Model.(*NotFoundResponse); ok {
				fmt.Printf("Parsed error model: %s\n", nf.Error)
			}
		} else {
			fmt.Printf("Non-API error: %v\n", err)
		}
	}

	// Example 2: Check for Cloudflare challenge
	_, err = request.GetTo[any](ctx, client, "/challenge-protected-page")
	if err != nil {
		if errors.Is(err, challenge.ErrCloudflareDetected) {
			fmt.Println("Cloudflare challenge detected, need browser-level solving")
		}
	}

	// Example 3: Check for response too large
	_, err = request.GetTo[any](ctx, client, "/large-payload")
	if err != nil {
		if errors.Is(err, aoni.ErrResponseTooLarge) {
			fmt.Println("Response exceeded configured size limit")
		}
	}

	fmt.Println("Error handling examples completed")
}
