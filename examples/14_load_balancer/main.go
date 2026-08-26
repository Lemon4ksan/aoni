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
	"github.com/lemon4ksan/aoni/option"
	lb "github.com/lemon4ksan/aoni/resiliency/loadbalancer"
)

type Response struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create a load balancer1 with weighted round-robin
	balancer1, err := lb.New(
		lb.Config{
			Strategy:   lb.WeightedRoundRobin,
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
	defer balancer1.Close()

	// Wrap the load balancer as the HTTP doer for a client
	client := aoni.NewClient(balancer1,
		option.WithBaseURL("https://httpbin.org"),
	)

	// Requests will be distributed across backends
	for i := range 6 {
		res, err := client.Get[Response](ctx, "/ip")
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}

		fmt.Printf("Request %d: served by %s\n", i, res.Origin)
	}

	// Update backends dynamically
	balancer1.UpdateBackends(
		"https://server-a.example.com",
		"https://server-b.example.com",
	)

	// RoundRobin strategy example
	balancer2, _ := lb.New(
		lb.Config{
			Strategy: lb.RoundRobin,
		},
		"https://api1.example.com",
		"https://api2.example.com",
	)
	defer balancer2.Close()

	_ = balancer2

	// Random strategy example
	balancer3, _ := lb.New(
		lb.Config{
			Strategy: lb.Random,
		},
		"https://cdn1.example.com",
		"https://cdn2.example.com",
	)
	defer balancer3.Close()

	_ = balancer3

	fmt.Println("Load balancer examples completed")
}
