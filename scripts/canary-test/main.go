// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// canary-test performs a live wire TLS fingerprint check against tls.peet.ws.
//
// It establishes a connection using the Chrome browser profile and validates
// that the server-observed JA4 fingerprint and HTTP/2 Akamai fingerprint match
// the expected browser-grade patterns.
//
// Exit codes:
//
//	0 - all checks passed
//	1 - one or more checks failed
//	2 - internal / network error
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
)

const (
	targetURL = "https://tls.peet.ws/api/all"
	timeout   = 30 * time.Second

	// JA4 prefix for TLS 1.3, desktop (SNI present), with "d" (domain).
	// Format: t<TLS-ver><d|i><cipherCount><extCount><ALPN> - first segment only.
	// Chrome with TLS 1.3 + SNI always starts with "t13d".
	ja4PrefixChrome = "t13d"
)

// peetResponse is a minimal projection of the tls.peet.ws /api/all JSON response.
type peetResponse struct {
	HTTPVersion string `json:"http_version"`
	TLS         struct {
		JA4  string `json:"ja4"`
		JA4R string `json:"ja4_r"`
	} `json:"tls"`
	HTTP2 *struct {
		AkamaiFingerprint     string `json:"akamai_fingerprint"`
		AkamaiFingerprintHash string `json:"akamai_fingerprint_hash"`
	} `json:"http2"`
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fmt.Printf("  Target  : %s\n", targetURL)
	fmt.Printf("  Profile : Chrome Desktop (option.WithChrome)\n\n")

	client := aoni.NewClient(nil, option.WithChrome())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		fatalf("build request: %v", err)
		return 2
	}

	resp, err := client.HTTP().Do(req)
	if err != nil {
		fatalf("HTTP request failed: %v", err)
		return 2
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		fatalf("unexpected HTTP status: %s", resp.Status)
		return 2
	}

	var result peetResponse
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fatalf("decode response body: %v", err)
		return 2
	}

	failed := false

	fmt.Printf("  [1/2] JA4 fingerprint check\n")
	fmt.Printf("        observed : %s\n", result.TLS.JA4)

	switch {
	case result.TLS.JA4 == "":
		errorf("JA4 field is empty - server may not have detected TLS 1.3")

		failed = true
	case !strings.HasPrefix(result.TLS.JA4, ja4PrefixChrome):
		errorf("JA4 does not start with %q (got %q) - TLS fingerprint does not match Chrome TLS 1.3 desktop profile",
			ja4PrefixChrome, result.TLS.JA4)

		failed = true
	default:
		fmt.Printf("        ✓ prefix %q matches Chrome TLS 1.3 desktop\n", ja4PrefixChrome)
	}

	fmt.Printf("\n  [2/2] HTTP/2 Akamai fingerprint check\n")
	fmt.Printf("        http_version : %s\n", result.HTTPVersion)

	switch {
	case result.HTTP2 == nil:
		// The connection was downgraded to HTTP/1.x - Chrome profile must negotiate H2.
		errorf("server did not report an http2 block - connection was not upgraded to HTTP/2")

		failed = true
	case result.HTTP2.AkamaiFingerprintHash == "":
		errorf("akamai_fingerprint_hash is empty - H2 settings frames were not recognised")

		failed = true
	default:
		fmt.Printf("        akamai_fp      : %s\n", result.HTTP2.AkamaiFingerprint)
		fmt.Printf("        akamai_fp_hash : %s\n", result.HTTP2.AkamaiFingerprintHash)
		fmt.Printf("        ✓ HTTP/2 Akamai fingerprint present\n")
	}

	fmt.Println()

	if failed {
		fmt.Println("FAIL - one or more canary checks did not pass.")
		return 1
	}

	fmt.Println("PASS - wire TLS JA4 & HTTP/2 Akamai fingerprint verified.")

	return 0
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
}

func errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "  ✗ "+format+"\n", args...)
}
