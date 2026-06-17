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
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni"
)

type Response struct {
	URL    string `json:"url"`
	Status string `json:"status"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org")

	// Retry on any error
	retryOnErr := aoni.Chain(
		client.HTTP(),
		aoni.RetryMiddleware(
			aoni.RetryOptions{
				MaxRetries:     3,
				Backoff:        1 * time.Second,
				JitterStrategy: aoni.JitterFull,
			},
			aoni.RetryOnErr(),
		),
	)

	// Retry on transient errors (network errors, 502, 503, 504)
	retryOnTransient := aoni.Chain(
		client.HTTP(),
		aoni.RetryMiddleware(
			aoni.RetryOptions{
				MaxRetries: 5,
				Backoff:    500 * time.Millisecond,
			},
			aoni.RetryOnTransientErrors(),
		),
	)

	// Custom retry condition: retry on specific status codes
	customRetry := aoni.Chain(
		client.HTTP(),
		aoni.RetryMiddleware(
			aoni.RetryOptions{MaxRetries: 3, Backoff: 2 * time.Second},
			func(resp *http.Response, err error) bool {
				if resp != nil && resp.StatusCode == 429 {
					fmt.Println("Rate limited, will retry...")
					return true
				}

				return false
			},
		),
	)

	// Use retryOnErr client
	retryClient := aoni.NewClient(retryOnErr).
		WithBaseURL("https://httpbin.org")

	res, err := aoni.GetJSON[Response](ctx, retryClient, "/status/200")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Success: %s %s\n", res.URL, res.Status)

	// Use customRetry client
	customClient := aoni.NewClient(customRetry).
		WithBaseURL("https://httpbin.org")

	_, _ = aoni.GetJSON[Response](ctx, customClient, "/status/429")

	// Use retryOnTransient client for unreliable endpoints
	_ = retryOnTransient

	fmt.Println("Retry middleware examples completed")
}
