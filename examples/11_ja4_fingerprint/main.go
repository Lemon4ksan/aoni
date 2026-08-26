// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: JA4 fingerprinting with TLS impersonation.
//
// Demonstrates WithTLSFingerprint for browser-like TLS handshakes,
// WithJA4Callback for capturing fingerprints, and TraceJA4 for
// reporting JA4 and JA4H identifiers.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/telemetry"
)

type Response struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create client with Chrome TLS fingerprint
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithJA4Callback(func(report ja4.Report) {
			fmt.Printf("JA4 callback: JA4=%s JA4H=%s proto=%s version=%s\n",
				report.JA4, report.JA4H, report.Protocol, report.Version)
			fmt.Printf("  SNI=%s ciphers=%d extensions=%d ALPN=%s\n",
				report.SNI, report.CipherCount, report.ExtCount, report.ALPN)
		}),
	)

	// Trace with JA4 fingerprint collection
	var info telemetry.TraceInfo

	_, err := client.Get[Response](ctx, "/ip",
		mod.WithTraceJA4(&info),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Print the collected JA4 report
	if info.JA4 != nil {
		fmt.Println("\n=== JA4 Fingerprint Report ===")
		printJA4Report(info.JA4)
	}

	// Firefox example
	firefoxClient := aoni.NewClient(nil,
		option.WithBaseURL("https://httpbin.org"),
		option.WithTLSFingerprint(aoni.BrowserFirefox),
		option.WithJA4Callback(func(report ja4.Report) {
			fmt.Printf("\nFirefox JA4: %s\n", report.JA4)
		}),
	)

	_, _ = firefoxClient.Get[Response](ctx, "/ip")
}

func printJA4Report(r *ja4.Report) {
	fmt.Printf("JA4:          %s\n", r.JA4)
	fmt.Printf("JA4H:         %s\n", r.JA4H)
	fmt.Printf("Protocol:     %s\n", r.Protocol)
	fmt.Printf("TLS Version:  %s\n", r.Version)
	fmt.Printf("SNI:          %s\n", r.SNI)
	fmt.Printf("Cipher Count: %d\n", r.CipherCount)
	fmt.Printf("Ext Count:    %d\n", r.ExtCount)
	fmt.Printf("ALPN:         %s\n", r.ALPN)
}
