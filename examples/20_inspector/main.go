// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Embedded Traffic Inspector
//
// Demonstrates how to spin up the built-in HTTP Traffic Inspector to view
// requests history, network timings timeline (DNS/TCP/TLS), and JA4 fingerprints.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/inspector"
)

func main() {
	client := aoni.NewClient(nil).
		WithTLSFingerprint(aoni.BrowserChrome)

	fmt.Println("==================================================")
	fmt.Println("Starting traffic inspector at http://127.0.0.1:8080")
	fmt.Println("==================================================")

	client, inspector, err := inspector.Enable(client, "127.0.0.1:8080")
	if err != nil {
		panic(err)
	}
	defer inspector.Close()

	fmt.Println("\n>>> ACTION REQUIRED <<<")
	fmt.Println("Open http://127.0.0.1:8080 in your browser to view the dashboard!")
	fmt.Println("Press Ctrl+C in this terminal when you want to exit.")
	fmt.Println("==================================================")
	fmt.Println()

	ctx := context.Background()
	i := 1
	for {
		fmt.Printf("[%s] Generating request %d to https://httpbin.org/headers...\n", time.Now().Format("15:04:05"), i)
		
		resp, err := client.Request(ctx, "GET", "https://httpbin.org/headers", func(req *http.Request) {
			req.Header.Set("X-Aoni-Request-Index", fmt.Sprintf("%d", i))
		})
		if err == nil {
			_ = resp.Body.Close()
		} else {
			fmt.Printf("Request failed: %v\n", err)
		}

		i++
		time.Sleep(5 * time.Second)
	}
}
