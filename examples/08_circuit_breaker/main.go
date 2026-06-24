// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Circuit breaker middleware.
//
// Demonstrates NewCircuitBreaker, CircuitBreakerMiddleware, and
// DefaultCircuitBreakerCondition for graceful degradation.
// Uses a sliding window: if 50%+ of requests within a 10s window
// fail, the circuit opens for the cooldown period.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
)

type StatusResponse struct {
	Status string `json:"status"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a circuit breaker: open when 50% of requests in a 10s window fail,
	// with a 30-second cooldown and minimum 5 requests before evaluation
	cb := aoni.NewCircuitBreaker(aoni.CircuitBreakerConfig{
		FailureThreshold: 0.5,
		MinRequests:      5,
		Cooldown:         30 * time.Second,
		Window:           10 * time.Second,
	})

	// Wrap client with circuit breaker middleware using the default condition
	doer := aoni.Chain(
		aoni.NewClient(nil).HTTP(),
		aoni.CircuitBreakerMiddleware(cb, aoni.DefaultCircuitBreakerCondition),
	)

	client := aoni.NewClient(doer).
		WithBaseURL("https://httpbin.org")

	// Make requests; once the failure ratio exceeds the threshold, the circuit opens
	// and all subsequent requests fail fast with a circuit-open error
	for i := 0; i < 10; i++ {
		res, err := aoni.GetJSON[StatusResponse](ctx, client, "/status/200")
		if err != nil {
			fmt.Printf("Request %d failed: %v\n", i, err)
			continue
		}

		fmt.Printf("Request %d: %s\n", i, res.Status)
	}

	fmt.Println("Circuit breaker example completed")
}
