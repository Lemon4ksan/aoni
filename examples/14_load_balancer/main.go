// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Load balancer with weighted round-robin strategy.
//
// Demonstrates NewLoadBalancer with WeightedRoundRobin, RoundRobin,
// and Random strategies for distributing requests across backends.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
)

type Response struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create a load balancer with weighted round-robin
	lb, err := aoni.NewLoadBalancer(
		aoni.LoadBalancerConfig{
			Strategy:   aoni.WeightedRoundRobin,
			MaxFails:   3,
			RetryAfter: 30 * time.Second,
		},
		"https://server1.example.com",
		"https://server2.example.com",
		"https://server3.example.com",
	)
	if err != nil {
		log.Fatal(err)
	}
	defer lb.Close()

	// Wrap the load balancer as the HTTP doer for a client
	client := aoni.NewClient(lb).
		WithBaseURL("https://httpbin.org")

	// Requests will be distributed across backends
	for i := 0; i < 6; i++ {
		res, err := aoni.GetJSON[Response](ctx, client, "/ip")
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}

		fmt.Printf("Request %d: served by %s\n", i, res.Origin)
	}

	// Update backends dynamically
	lb.UpdateBackends(
		"https://server-a.example.com",
		"https://server-b.example.com",
	)

	// RoundRobin strategy example
	lb2, _ := aoni.NewLoadBalancer(
		aoni.LoadBalancerConfig{
			Strategy: aoni.RoundRobin,
		},
		"https://api1.example.com",
		"https://api2.example.com",
	)
	defer lb2.Close()

	_ = lb2

	// Random strategy example
	lb3, _ := aoni.NewLoadBalancer(
		aoni.LoadBalancerConfig{
			Strategy: aoni.Random,
		},
		"https://cdn1.example.com",
		"https://cdn2.example.com",
	)
	defer lb3.Close()

	_ = lb3

	fmt.Println("Load balancer examples completed")
}
