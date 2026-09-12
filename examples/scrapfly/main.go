// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	"github.com/lemon4ksan/aoni/option"
)

type JA3Resp struct {
	JA3Hash string `json:"ja3_hash"`
	JA4     string `json:"ja4"`
	JA3N    string `json:"ja3n_hash"`
}

type HTTP2Resp struct {
	Fingerprint string `json:"http2_fingerprint"`
}

func main() {
	fmt.Println("=== Aoni Network Protocol Evasion Audit ===")
	fmt.Println("Target: Scrapfly Web Scraping Tools API (https://tools.scrapfly.io)")

	browserProfiles := []struct {
		name string
		id   aoni.BrowserID
	}{
		{"Google Chrome", aoni.BrowserChrome},
		{"Mozilla Firefox", aoni.BrowserFirefox},
		{"Apple Safari", aoni.BrowserSafari},
		{"Standard Go (No Evasion)", aoni.BrowserNone},
	}

	for _, profile := range browserProfiles {
		fmt.Printf("\n--- Testing Profile: %s ---\n", profile.name)

		// Create a fast engine client with full browser profile (TLS + HTTP2/HTTP3 + Headers)
		opts := []aoni.ClientOption{}
		if profile.id != aoni.BrowserNone {
			opts = append(opts, option.WithBrowserProfile(profile.id, profiles.Windows))
		}

		client := aoni.NewClient(
			fast.NewClient(opts...),
		)

		ctx := context.Background()

		// Test tls.peet.ws API
		resp, err := client.Get(ctx, "https://tls.peet.ws/api/all")
		if err != nil {
			log.Printf("[%s] Fetch Error: %v\n", profile.name, err)
		} else {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var p struct {
				TLS struct {
					JA3     string `json:"ja3"`
					JA3Hash string `json:"ja3_hash"`
				} `json:"tls"`
				HTTP2 struct {
					AkamaiFingerprint string `json:"akamai_fingerprint"`
				} `json:"http2"`
			}
			json.Unmarshal(bodyBytes, &p)

			fmt.Printf("[TLS] JA3 Hash: %s\n", p.TLS.JA3Hash)
			fmt.Printf("[HTTP2] Akamai FP: %s\n", p.HTTP2.AkamaiFingerprint)
		}
	}
}
