// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Request tracing and curl command generation.
//
// Demonstrates Trace(&info) for timing measurements (DNS, TCP, TLS, TTFB),
// TraceJA4(&info) for JA4 fingerprinting, and AsCurl() for curl reproduction.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/aoni/telemetry"

	"github.com/lemon4ksan/aoni"
)

type HTTPBinResponse struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
	)

	// Trace with full timing breakdown
	var info telemetry.TraceInfo

	_, err := request.GetTo[HTTPBinResponse](ctx, client, "/ip",
		mod.WithTrace(&info),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Timing Breakdown ===")
	fmt.Printf("DNS Lookup:    %v\n", info.DNSLookup)
	fmt.Printf("TCP Connect:   %v\n", info.TCPConn)
	fmt.Printf("TLS Handshake: %v\n", info.TLSHandshake)
	fmt.Printf("Server Wait:   %v\n", info.ServerProcessing)
	fmt.Printf("Content Transfer: %v\n", info.ContentTransfer)
	fmt.Printf("Total:         %v\n", info.Total)
	fmt.Printf("Request Size:  %d bytes\n", info.RequestSize)
	fmt.Printf("Response Size: %d bytes\n", info.ResponseSize)

	// AsCurl + CaptureResponse: capture the raw response for curl generation
	var resp *http.Response

	_, err = request.GetTo[HTTPBinResponse](ctx, client, "/ip",
		mod.WithCurlDump(),
		mod.WithCaptureResponse(&resp),
	)
	if err != nil {
		log.Fatal(err)
	}

	if resp != nil {
		fmt.Printf("\nCaptured response status: %d\n", resp.StatusCode)
	}

	// Generate curl command from a request
	curlCmd := telemetry.CurlFromRequest(nil, nil)
	fmt.Printf("\nCurl command generator: %s\n", curlCmd)
}
