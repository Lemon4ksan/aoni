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
	"github.com/lemon4ksan/aoni/ja4"
)

type Response struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create client with Chrome TLS fingerprint
	client := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org").
		WithTLSFingerprint(aoni.BrowserChrome).
		WithJA4Callback(func(report ja4.Report) {
			fmt.Printf("JA4 callback: JA4=%s JA4H=%s proto=%s version=%s\n",
				report.JA4, report.JA4H, report.Protocol, report.Version)
			fmt.Printf("  SNI=%s ciphers=%d extensions=%d ALPN=%s\n",
				report.SNI, report.CipherCount, report.ExtCount, report.ALPN)
		})

	// Trace with JA4 fingerprint collection
	var info aoni.TraceInfo

	_, err := aoni.GetJSON[Response](ctx, client, "/ip",
		aoni.TraceJA4(&info),
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
	firefoxClient := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org").
		WithTLSFingerprint(aoni.BrowserFirefox).
		WithJA4Callback(func(report ja4.Report) {
			fmt.Printf("\nFirefox JA4: %s\n", report.JA4)
		})

	_, _ = aoni.GetJSON[Response](ctx, firefoxClient, "/ip")
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
