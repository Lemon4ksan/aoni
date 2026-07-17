// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/inspector"
	"github.com/lemon4ksan/aoni/p0f"
)

// ProtectedUserData describes the target data structure using custom aoni values.
type ProtectedUserData struct {
	ID        aoni.Uint64String  `json:"id"`
	Name      string             `json:"name"`
	IsActive  aoni.BoolInt       `json:"is_active"`
	CreatedAt aoni.UnixTimestamp `json:"created_at"`
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
	mockResponse.Header.Set("Content-Type", "application/json")
	return mockResponse, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// ==========================================
	// PHASE 1: SQLite Cookie DB & DNS Resolvers
	// ==========================================

	// Open the isolated sqlite3 cookie database.
	db, err := sql.Open("sqlite3", "./aoni_secure_cookies.db")
	if err != nil {
		log.Fatalf("Cookie DB connection error: %v", err)
	}
	defer db.Close()

	sqlStorage := aoni.NewSQLCookieStorage(db)
	// Init schema works but uses dummy no-op driver statements here.
	_ = sqlStorage.InitSchema()

	cookieJar := aoni.NewProxyIsolatedCookieJar().WithStorageBackend(sqlStorage)

	// High-speed race DNS resolver.
	raceResolver := aoni.NewFastRaceResolver(
		aoni.NewDoHResolver("https://1.1.1.1/dns-query", "cloudflare-dns.com"),
		aoni.NewDoTResolver("8.8.8.8:853", "dns.google"),
	)

	// HAR traffic recorder.
	harGenerator := aoni.NewHARGenerator()

	// ==========================================
	// PHASE 2: Core Client Configuration
	// ==========================================

	client := aoni.NewClient(nil).
		WithBaseURL("https://api.protected-target.com").
		WithTimeout(30 * time.Second).
		WithDNSResolver(raceResolver).
		WithProxyIsolatedCookieJar(cookieJar).
		WithDynamicHedging(nil). // Enables dynamic tail-latency reduction (hedging)
		WithChallengeSolver(&HeadlessWAFSolver{}).
		WithP0fSignature(p0f.Windows10). // Emulate Windows 10 TCP/IP stack
		WithPacketPadding(aoni.PaddingConfig{
			MaxSegmentSize:  512, // Lower MSS to force packet fragmentation
			MinPaddingBytes: 32,
			MaxPaddingBytes: 128,
			HeaderPool:      aoni.CloudflareHeaderPool, // Disguise padding as Cloudflare headers
		})

	// Start traffic inspector dashboard.
	client, inspector, err := inspector.Enable(client, "127.0.0.1:8080")
	if err != nil {
		panic(err)
	}
	defer inspector.Close()

	// ==========================================
	// PHASE 3: Custom Pipeline Middleware Wrapper
	// ==========================================

	proxies := []string{
		"socks5://user:pass@185.120.10.1:1080",
		"socks5://user:pass@185.120.10.2:1080",
		"http://user:pass@190.45.20.1:8080",
	}
	cacheStore := aoni.NewInMemoryCacheStore()

	client = client.WithPipelineWrapper(func(c *aoni.Client, engine aoni.HTTPDoer) aoni.HTTPDoer {
		chain := engine

		// 1. User Agent and Hints rotation
		chain = aoni.UserAgentAndHintsRotationMiddleware(nil)(chain)

		// 2. DPI timing evasion jitter
		chain = aoni.DPIJitterMiddleware(50*time.Millisecond, 250*time.Millisecond)(chain)

		// 3. Proxy failover and automatic retry rotation
		chain = aoni.ProxyFailoverMiddleware(proxies, 2)(chain)

		// 4. Hedging (tail latency reduction)
		chain = aoni.HedgingMiddleware(c.Network().HedgingDelay, c.Network().DynamicHedging)(chain)

		// 5. GET request caching
		chain = aoni.CacheMiddleware(cacheStore, 10*time.Minute)(chain)

		// 6. HAR generation, data redaction, and traffic capture
		chain = aoni.HARGeneratorMiddleware(harGenerator)(chain)
		chain = aoni.SensitiveDataRedactorMiddleware([]string{"Authorization", "Cookie", "X-Api-Key"}, nil)(chain)
		chain = aoni.InspectorMiddleware(c.Inspector())(chain)

		// 7. Core HTTP response verification and decompression
		chain = aoni.ResponseSizeLimitMiddleware(c.Defaults().MaxResponseSize)(chain)
		chain = aoni.DecompressionAndTranscodingMiddleware()(chain)
		chain = aoni.MultiReadBodyMiddleware(c.Defaults().MaxResponseSize, c.Defaults().MultiReadDisableDisk)(chain)
		chain = aoni.ResponseValidationMiddleware()(chain)
		chain = aoni.ChallengeSolverMiddleware(c.Defaults().ChallengeSolver, c.Defaults().ChallengeDetector)(chain)

		// 8. Connection cleanup, hooks execution, and request context injection
		chain = aoni.FinalizerMiddleware()(chain)
		chain = aoni.HooksMiddleware(c.Defaults().BeforeRequest, c.Defaults().AfterResponse)(chain)
		chain = aoni.ContextMiddleware(c)(chain)

		return chain
	})

	// ==========================================
	// PHASE 4: Safe Execution
	// ==========================================

	log.Println("Executing safe user profile lookup...")

	antiBotValidator := aoni.WithResponseValidator(func(resp *http.Response) error {
		if resp.Header.Get("X-Frame-Options") == "DENY" {
			return errors.New("proxy address blocked by target system")
		}
		return nil
	})

	userData, rawResp, err := aoni.GetToEx[ProtectedUserData](
		ctx,
		client,
		"/v1/users/profile",
		antiBotValidator,
		aoni.WithHeader("X-Client-Build", "release-v2.0"),
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
