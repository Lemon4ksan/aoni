// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: Browser impersonation with Chrome and Firefox profiles.
//
// Demonstrates importing profiles/chrome and profiles/firefox to get
// browser-specific TLS specs, User-Agent strings, and platform constants.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/profiles/chrome"
	"github.com/lemon4ksan/aoni/profiles/firefox"
)

type Response struct {
	Origin string `json:"origin"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Chrome desktop profile
	fmt.Println("=== Chrome Desktop ===")
	fmt.Printf("User-Agent: %s\n", chrome.UserAgentWindows)
	fmt.Printf("Sec-CH-UA:  %s\n", chrome.SecCHUA)
	fmt.Printf("Platform:   %s\n", chrome.PlatformWindows)

	chromeClient := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org").
		WithTLSFingerprint(aoni.BrowserChrome).
		WithUserAgent(chrome.UserAgentWindows)

	res, err := aoni.GetJSON[Response](ctx, chromeClient, "/ip")
	if err != nil {
		fmt.Printf("Chrome request failed: %v\n", err)
	} else {
		fmt.Printf("Chrome request: %s\n", res.Origin)
	}

	// Chrome mobile profile
	fmt.Println("\n=== Chrome Mobile ===")
	fmt.Printf("User-Agent: %s\n", chrome.UserAgentAndroid)
	fmt.Printf("Platform:   %s\n", chrome.PlatformAndroid)

	// Firefox desktop profile
	fmt.Println("\n=== Firefox Desktop ===")
	fmt.Printf("User-Agent: %s\n", firefox.UserAgentFirefoxWindows)

	firefoxClient := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org").
		WithTLSFingerprint(aoni.BrowserFirefox).
		WithUserAgent(firefox.UserAgentFirefoxWindows)

	res, err = aoni.GetJSON[Response](ctx, firefoxClient, "/ip")
	if err != nil {
		fmt.Printf("Firefox request failed: %v\n", err)
	} else {
		fmt.Printf("Firefox request: %s\n", res.Origin)
	}

	// Firefox mobile profile
	fmt.Println("\n=== Firefox Mobile ===")
	fmt.Printf("User-Agent: %s\n", firefox.UserAgentFirefoxAndroid)

	// Profile variants contain full header ordering and TLS specs
	fmt.Println("\n=== Chrome Variants ===")
	fmt.Printf("Desktop TLS spec available: %v\n", chrome.Desktop != nil)
	fmt.Printf("Mobile TLS spec available:  %v\n", chrome.Mobile != nil)
	fmt.Printf("Chrome boundary: %s\n", chrome.Boundary())

	fmt.Println("\n=== Firefox Variants ===")
	fmt.Printf("Desktop TLS spec available: %v\n", firefox.Desktop != nil)
	fmt.Printf("Mobile TLS spec available:  %v\n", firefox.Mobile != nil)
	fmt.Printf("Firefox boundary: %s\n", firefox.Boundary())

	// Combine profiles for realistic browser impersonation
	fmt.Println("\n=== Full Impersonation Example ===")

	combined := aoni.NewClient(nil).
		WithBaseURL("https://httpbin.org").
		WithTLSFingerprint(aoni.BrowserChrome).
		WithUserAgent(chrome.UserAgentWindows).
		WithOrigin("https://www.google.com").
		WithHeader("Sec-CH-UA", chrome.SecCHUA).
		WithHeader("Sec-CH-UA-Platform", chrome.PlatformWindows).
		WithConnectionPool(aoni.ConnectionPoolConfig{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		})
	_ = combined

	fmt.Println("Browser impersonation examples completed")
}
