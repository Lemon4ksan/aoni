// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Example: WAF/Cloudflare Challenge Solver interface.
//
// Demonstrates how to register a ChallengeSolver on the Client to intercept
// ErrCloudflareChallenge blocks, automatically spin up a browser in the background
// to bypass the check, extract the cookies, and resume Go-level requests transparently.
package main

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
)

// BrowserChallengeSolver implements aoni.ChallengeSolver using a conceptual
// headless browser driver (e.g. Playwright / Puppeteer).
type BrowserChallengeSolver struct{}

func (s *BrowserChallengeSolver) Solve(ctx context.Context, _ error, req *http.Request) (*http.Response, error) {
	fmt.Printf("[Solver] Intercepted blocked request to: %s\n", req.URL)
	fmt.Println("[Solver] Starting headless browser (Playwright) in background...")

	// Conceptual Implementation:
	//
	// pw, err := playwright.Run()
	// browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
	//     Headless: playwright.Bool(true),
	// })
	// page, err := browser.NewPage()
	// _, err = page.Goto(req.URL.String())
	//
	// // Wait for challenge to solve
	// _, err = page.WaitForSelector("#content-solved-marker", playwright.PageWaitForSelectorOptions{
	//     Timeout: playwright.Float(15000),
	// })
	//
	// cookies, err := page.Context().Cookies()
	// // Set these cookies back into aoni Client's CookieJar or the request Headers
	// ...

	fmt.Println("[Solver] Cloudflare check passed! Solved cookies extracted.")
	fmt.Println("[Solver] Replaying original request with browser credentials...")

	// Construct solved request
	solvedReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
	if err != nil {
		return nil, err
	}

	// Copy original headers
	maps.Copy(solvedReq.Header, req.Header)

	// Inject solved cookie/credentials
	solvedReq.Header.Set(header.Cookie, "cf_clearance=solved_value_abc123")
	solvedReq.Header.Set(header.UserAgent, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) ...")

	// Send request via default transport
	client := &http.Client{}
	return client.Do(solvedReq)
}

func main() {
	_ = context.Background()
	_ = time.Second

	solver := &BrowserChallengeSolver{}

	_ = aoni.NewClient(nil, option.WithChallengeSolver(solver))

	// In a real application, if this request hits a Cloudflare check:
	// 1. Aoni receives the challenge response (ErrCloudflareChallenge).
	// 2. Aoni intercepts the response and calls solver.Solve().
	// 3. solver.Solve() solves the challenge, attaches the solved cookies,
	//    and returns the successful response.
	// 4. Aoni decodes the response seamlessly.
	fmt.Println("ChallengeSolver interface compiled and ready.")
}
