// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/dns"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/resiliency/cache"
	"github.com/lemon4ksan/aoni/telemetry"
)

// ProtectedUserData describes the target data structure using custom aoni values.
type ProtectedUserData struct {
	ID        values.Uint64String  `json:"id"`
	Name      string               `json:"name"`
	IsActive  values.BoolInt       `json:"is_active"`
	CreatedAt values.UnixTimestamp `json:"created_at"`
}

// HeadlessWAFSolver simulates an external solver (Playwright/Puppeteer/Selenium) handling WAF challenges.
type HeadlessWAFSolver struct{}

// Solve intercepts challenges, simulates browser environment resolution, and returns a successful response.
func (s *HeadlessWAFSolver) Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error) {
	log.Printf("[WAF Solver] WAF challenge detected: %v. Launching browser simulation...", err)

	// Simulate successful challenge resolution returning CF clearance cookies.
	mockResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id": "1002302", "name": "Stealth User", "is_active": 1, "created_at": "1704067200"}`)),
		Header:     make(http.Header),
		Request:    req,
	}
	mockResponse.Header.Set(fheader.ContentType, fheader.MIMEApplicationJSON)
	return mockResponse, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// ==========================================
	// PHASE 1: Atomic Cookie Storage & DNS Resolvers
	// ==========================================

	// Use atomic JSON file storage for proxy-isolated cookies.
	fileStorage := cookie.NewJSONFileStorage("./aoni_secure_cookies.json")
	cookieJar := cookie.NewProxyIsolatedJar().WithStorageBackend(fileStorage)

	// High-speed race DNS resolver.
	raceResolver := dns.NewFastRaceResolver(
		dns.NewDoHResolver("https://1.1.1.1/dns-query", "cloudflare-dns.com", nil),
		dns.NewDoTResolver("8.8.8.8:853", "dns.google"),
	)

	// HAR traffic recorder.
	harGenerator := telemetry.NewHARGenerator()

	// ==========================================
	// PHASE 2: Core Client Configuration
	// ==========================================

	client := aoni.NewClient(nil,
		option.WithBaseURL("https://api.protected-target.com"),
		option.WithTimeout(30*time.Second),
		option.WithDNSResolver(raceResolver),
		option.WithCookieJar(cookieJar),
		option.WithDynamicHedging(nil),         // Enables dynamic tail-latency reduction (hedging)
		option.WithP0fSignature(p0f.Windows10), // Emulate Windows 10 TCP/IP stack
		option.WithPacketPadding(fingerprint.PaddingConfig{
			MaxSegmentSize:  512, // Lower MSS to force packet fragmentation
			MinPaddingBytes: 32,
			MaxPaddingBytes: 128,
			HeaderPool:      fingerprint.CloudflareHeaderPool, // Disguise padding as Cloudflare headers
		}),
		option.WithChallengeSolver(&HeadlessWAFSolver{}),
	)

	// ==========================================
	// PHASE 3: Custom Pipeline Configuration
	// ==========================================

	proxies := []string{
		"socks5://user:pass@185.120.10.1:1080",
		"socks5://user:pass@185.120.10.2:1080",
		"http://user:pass@190.45.20.1:8080",
	}
	cacheStore := cache.NewInMemoryStore(5 * time.Minute)

	client = client.With(option.WithPipeline(aoni.PipelineConfig{
		RotateUA: true,
		DPIJitter: &aoni.DPIJitterConfig{
			MinDelay: 50 * time.Millisecond,
			MaxDelay: 250 * time.Millisecond,
		},
		ProxyFailover: &aoni.ProxyFailoverConfig{
			Proxies:    proxies,
			RetryLimit: 2,
		},
		Hedging: &aoni.HedgingConfig{
			DefaultDelay:   client.Network().HedgingDelay,
			DynamicHedging: client.Network().DynamicHedging,
		},
		Cache: &aoni.CacheConfig{
			Store:      cacheStore,
			DefaultTTL: 10 * time.Minute,
		},
		HAR: &aoni.HARConfig{
			Tracker: harGenerator,
		},
		Redact: &aoni.RedactConfig{
			HeadersToRedact: []string{"Authorization", "Cookie", "X-Api-Key"},
		},
		Inspect:    true,
		SizeLimit:  client.Defaults().MaxResponseSize,
		Decompress: true,
		Validate:   true,
		Challenge:  true,
	}))

	// ==========================================
	// PHASE 4: Safe Execution
	// ==========================================

	log.Println("Executing safe user profile lookup...")

	antiBotValidator := mod.WithResponseValidator(func(resp *http.Response) error {
		if resp.Header.Get("X-Frame-Options") == "DENY" {
			return errors.New("proxy address blocked by target system")
		}
		return nil
	})

	userData, rawResp, err := client.FetchEx[ProtectedUserData](
		ctx, http.MethodGet, "/v1/users/profile", nil,
		antiBotValidator,
		mod.WithHeader("X-Client-Build", "release-v2.0"),
	)

	if err != nil {
		var apiErr *aoni.APIError
		if errors.As(err, &apiErr) {
			log.Printf("Server returned API error (Status %d): %s", apiErr.StatusCode, string(apiErr.Body))
			return
		}
		log.Fatalf("Request failed: %v", err)
	}

	// Print successfully resolved profile data.
	log.Printf("Successfully retrieved user details (Proxy: %s):", rawResp.Request.URL.Host)
	fmt.Printf("User ID: %d\n", uint64(userData.ID))
	fmt.Printf("Name: %s\n", userData.Name)
	fmt.Printf("Is Active: %v\n", bool(userData.IsActive))
	fmt.Printf("Created At: %s\n", userData.CreatedAt.Time().Format(time.RFC1123))

	// ==========================================
	// PHASE 5: Save Log Archives
	// ==========================================

	harBytes, err := harGenerator.Export()
	if err == nil {
		_ = os.WriteFile("session_traffic.har", harBytes, 0644)
		log.Println("Session traffic successfully saved to session_traffic.har")
	}
}
