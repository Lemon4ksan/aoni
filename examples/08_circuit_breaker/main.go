// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Circuit breaker middleware.
//
// Demonstrates NewCircuitBreaker, CircuitBreakerMiddleware, and
// DefaultCircuitBreakerCondition for graceful degradation.
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

	// Create a circuit breaker: open after 5 failures, close after 3 successes,
	// with a 30-second cooldown
	cb := aoni.NewCircuitBreaker(aoni.CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Cooldown:         30 * time.Second,
	})

	// Wrap client with circuit breaker middleware using the default condition
	doer := aoni.Chain(
		aoni.NewClient(nil).HTTP(),
		aoni.CircuitBreakerMiddleware(cb, aoni.DefaultCircuitBreakerCondition),
	)

	client := aoni.NewClient(doer).
		WithBaseURL("https://httpbin.org")

	// Make requests; once too many failures occur, the circuit opens
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
