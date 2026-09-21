// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Retry middleware with RetryOptions and retry conditions.
//
// Demonstrates RetryMiddleware with RetryOnErr(), RetryOnTransientErrors(),
// and custom retry conditions with exponential backoff.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/option"
)

type Response struct {
	URL    string `json:"url"`
	Status string `json:"status"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Configure retry on any error via option.WithMiddleware
	retryClient := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
		option.WithMiddleware(
			middleware.Retry(
				middleware.RetryOptions{
					MaxRetries:     3,
					Backoff:        1 * time.Second,
					JitterStrategy: middleware.JitterFull,
				},
				middleware.RetryOnErr(),
			),
		),
	)

	res, err := retryClient.GetTo[Response](ctx, "/status/200")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Success: %s %s\n", res.URL, res.Status)

	// 2. Fluent chaining via client.Use with custom retry condition
	customClient := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
	).Use(
		middleware.Retry(
			middleware.RetryOptions{MaxRetries: 3, Backoff: 2 * time.Second},
			func(resp aoni.Response, err error) bool {
				if resp != nil && resp.StatusCode() == 429 {
					fmt.Println("Rate limited, will retry...")
					return true
				}
				return false
			},
		),
	)

	_, _ = customClient.GetTo[Response](ctx, "/status/429")

	// 3. Transient errors retry client for unreliable network paths
	transientClient := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
	).Use(
		middleware.Retry(
			middleware.RetryOptions{
				MaxRetries: 5,
				Backoff:    500 * time.Millisecond,
			},
			middleware.RetryOnTransientErrors(),
		),
	)
	_ = transientClient

	fmt.Println("Retry middleware examples completed")
}
