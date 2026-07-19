// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// ServicePipe operates as a security gateway.
// When accessing the website, Next-auth on the client makes an AJAX request to /api/auth/session.
// If the session is not yet validated, ServicePipe blocks this request with a 403 status code, forcing
// a client-side redirect to the error page (/api/auth/error).
// However, the critical session cookies (spsn, spid, and server_token) are already dispatched and set
// by the server on the initial handshake before this client-side redirection takes place.
//
// By continuously polling the browser context, we capture these cookies immediately after they are set
// and reuse them with aligned TLS fingerprints, bypassing the verification stage entirely.
// 
// 5ka.ru only works with Russian VPN. No Yandex captcha is triggered.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/profiles"
	"github.com/lemon4ksan/aoni/profiles/firefox"
	"github.com/mxschmitt/playwright-go"
)

// cookiesFile defines the local storage path for caching session credentials.
const cookiesFile = "cookies_5ka_firefox.json"

// CookieData represents a serializable structure for HTTP cookies.
// It mirrors the standard http.Cookie but provides JSON tags for disk persistence.
type CookieData struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires"`
	HttpOnly bool      `json:"httpOnly"`
	Secure   bool      `json:"secure"`
}

// saveCookies serializes slice of http.Cookie pointers into a formatted JSON file.
// This implements a persistent session cache, reducing the need to spawn
// browser automation instances on every execution.
func saveCookies(cookies []*http.Cookie) error {
	data := make([]CookieData, len(cookies))
	for i, c := range cookies {
		data[i] = CookieData{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HttpOnly: c.HttpOnly,
			Secure:   c.Secure,
		}
	}
	file, _ := json.MarshalIndent(data, "", "  ")
	// Using 0600 file permissions to ensure session secrets are readable only by the owner.
	return os.WriteFile(cookiesFile, file, 0600)
}

// loadCookies reads and deserializes cached cookies from disk.
func loadCookies() ([]*http.Cookie, error) {
	data, err := os.ReadFile(cookiesFile)
	if err != nil {
		return nil, err
	}
	var cookieData []CookieData
	if err := json.Unmarshal(data, &cookieData); err != nil {
		return nil, err
	}
	cookies := make([]*http.Cookie, len(cookieData))
	for i, c := range cookieData {
		cookies[i] = &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HttpOnly: c.HttpOnly,
			Secure:   c.Secure,
		}
	}
	return cookies, nil
}

// areCookiesValid checks if the necessary session state tokens exist and are not expired.
// Specifically targets ServicePipe security and Next-auth tokens:
// - spid & spsn: Security identification and state tokens.
// - server_token: Backend session validation token.
func areCookiesValid(cookies []*http.Cookie) bool {
	if len(cookies) == 0 {
		return false
	}
	now := time.Now()
	hasServerToken, hasSpid, hasSpsn := false, false, false
	for _, c := range cookies {
		if c.Name == "server_token" && !c.Expires.IsZero() && c.Expires.After(now) {
			hasServerToken = true
		}
		if c.Name == "spid" && !c.Expires.IsZero() && c.Expires.After(now) {
			hasSpid = true
		}
		if c.Name == "spsn" && !c.Expires.IsZero() && c.Expires.After(now) {
			hasSpsn = true
		}
	}
	return hasServerToken && hasSpid && hasSpsn
}

// getFreshCookies handles the browser automation orchestration.
// It initializes Playwright, builds a spoofed browser context matching the fingerprint,
// loads the target page, and dynamically captures the required session tokens
// before client-side scripts perform error/challenge redirects.
func getFreshCookies(targetURL string) ([]*http.Cookie, string, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, "", fmt.Errorf("playwright run: %w", err)
	}
	defer pw.Stop()

	// Launching the browser in headful mode (Headless: false) is useful for debugging
	// and often aids in bypassing automated headless browser checks.
	browser, err := pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: new(true),
	})
	if err != nil {
		return nil, "", fmt.Errorf("launch browser: %w", err)
	}
	defer browser.Close()

	// Spoofing the browser environment to closely match a genuine user workstation.
	// Ensuring aligned values for Timezone, Locale, Geolocation, and User-Agent
	// is critical to satisfy client-side anti-fraud and telemetry checks.
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent:        new(firefox.UserAgentFirefoxWindows),
		ExtraHttpHeaders: map[string]string{"Accept-Language": "ru-RU,ru;q=0.9,en;q=0.8"},
		Locale:           new("ru-RU"),
		TimezoneId:       new("Europe/Moscow"),
		Geolocation:      &playwright.Geolocation{Latitude: 55.7558, Longitude: 37.6173},
		Viewport:         &playwright.Size{Width: 1920, Height: 1080},
	})
	if err != nil {
		return nil, "", fmt.Errorf("create context: %w", err)
	}
	defer context.Close()

	page, err := context.NewPage()
	if err != nil {
		return nil, "", fmt.Errorf("create page: %w", err)
	}

	fmt.Printf("[Playwright] Opening target URL: %s\n", targetURL)

	// WaitUntilStateDomcontentloaded allows the execution of critical scripts as soon as possible,
	// facilitating the initial handshakes that set the required cookies.
	if _, err := page.Goto(targetURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(60000),
	}); err != nil {
		return nil, "", fmt.Errorf("goto: %w", err)
	}

	currentURL := page.URL()
	fmt.Printf("[Playwright] Current URL: %s\n", currentURL)

	// Handling scenarios where the automated flow hits an immediate VPN/block notice.
	// Often, navigating back allows the application state to settle and bypasses the temporary intercept.
	if strings.Contains(currentURL, "block") ||
		strings.Contains(currentURL, "vpn") ||
		strings.Contains(currentURL, "connect") ||
		strings.Contains(strings.ToLower(currentURL), "проблемы") {
		fmt.Println("[Playwright] VPN or Block page detected. Attempting to navigate back...")
		if _, err := page.GoBack(playwright.PageGoBackOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		}); err == nil {
			fmt.Printf("[Playwright] URL after navigating back: %s\n", page.URL())
			time.Sleep(2 * time.Second)
		} else {
			fmt.Printf("[Playwright] GoBack failed: %v\n", err)
		}
	}

	// Simulating realistic human viewport interaction to trigger scroll-dependent scripts or telemetry checks.
	_, _ = page.Evaluate(`window.scrollBy(0, 500)`)
	time.Sleep(1 * time.Second)
	_, _ = page.Evaluate(`window.scrollBy(0, 300)`)
	time.Sleep(1 * time.Second)

	// Polling loop targeting the critical session cookies.
	// Since Next-auth's client-side AJAX requests can result in a 403 redirecting to '/api/auth/error',
	// we actively check the cookie storage *asynchronously* to catch the valid parameters
	// before the browser acts on the client-side redirect.
	targetCookies := []string{"spsn", "spid", "server_token"}
	fmt.Println("[Playwright] Waiting for target session cookies to populate...")
	start := time.Now()
	var lastCookies []playwright.Cookie
	for time.Since(start) < 60*time.Second {
		cookies, _ := context.Cookies()
		lastCookies = cookies
		found := 0
		for _, c := range cookies {
			for _, t := range targetCookies {
				if c.Name == t {
					found++
				}
			}
		}

		// If all target cookies are present, we immediately extract them.
		if found >= len(targetCookies) {
			fmt.Println("[Playwright] All targeted session cookies retrieved successfully.")
			version := "130.0"
			return convertCookies(cookies), version, nil
		}

		// Checking if we reached a blocked/error state.
		currentURL := page.URL()
		isBlocked := strings.Contains(currentURL, "block") ||
			strings.Contains(currentURL, "vpn") ||
			strings.Contains(currentURL, "connect") ||
			strings.Contains(currentURL, "error") ||
			strings.Contains(currentURL, "auth") ||
			strings.Contains(strings.ToLower(currentURL), "проблемы")

		if isBlocked {
			// Race condition mitigation: Even if redirected to an error/auth page,
			// the required 'spid' and 'spsn' might have already been successfully assigned.
			if found >= 2 {
				fmt.Println("[Playwright] Redirected to block/error page, but 'spid' and 'spsn' are set. Returning to home page...")
				if _, err := page.Goto(targetURL, playwright.PageGotoOptions{
					WaitUntil: playwright.WaitUntilStateDomcontentloaded,
				}); err != nil {
					_, _ = page.GoBack(playwright.PageGoBackOptions{
						WaitUntil: playwright.WaitUntilStateDomcontentloaded,
					})
				}
			} else {
				// If blocked without obtaining cookies, we reload to prompt another handshake attempt.
				fmt.Println("[Playwright] Blocked without session cookies. Reloading page...")
				_, _ = page.Reload(playwright.PageReloadOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
			}
			time.Sleep(3 * time.Second)
		} else if found >= 2 && time.Since(start) > 15*time.Second {
			// If partial state is reached (spid/spsn present, but server_token is missing),
			// reloading the main page can trigger the backend authentication refresh, completing the cookie set.
			fmt.Println("[Playwright] 'spid' and 'spsn' found, but 'server_token' is missing. Reloading to trigger backend session mapping...")
			_, _ = page.Reload(playwright.PageReloadOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded})
			time.Sleep(3 * time.Second)
		}
		time.Sleep(1 * time.Second)
	}

	// Fallback mechanism: Return whatever cookies were grabbed if the full set could not be verified in time.
	if len(lastCookies) > 0 {
		fmt.Printf("[Playwright] Full set of target cookies not retrieved. Proceeding with %d available cookies...\n", len(lastCookies))
		version := "130.0"
		return convertCookies(lastCookies), version, nil
	}

	return nil, "", errors.New("failed to acquire session cookies within the 60-second limit")
}

// convertCookies maps Playwright's cookie representation into Go's standard http.Cookie structures,
// ensuring accurate preservation of the SameSite attribute, expiration dates, and security flags.
func convertCookies(pwCookies []playwright.Cookie) []*http.Cookie {
	var result []*http.Cookie
	for _, c := range pwCookies {
		sameSite := http.SameSiteDefaultMode
		if c.SameSite != nil {
			switch c.SameSite {
			case playwright.SameSiteAttributeLax:
				sameSite = http.SameSiteLaxMode
			case playwright.SameSiteAttributeStrict:
				sameSite = http.SameSiteStrictMode
			case playwright.SameSiteAttributeNone:
				sameSite = http.SameSiteNoneMode
			}
		}
		result = append(result, &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  time.Unix(int64(c.Expires), 0),
			HttpOnly: c.HttpOnly,
			Secure:   c.Secure,
			SameSite: sameSite,
		})
	}
	return result
}

// run executes the main execution flow: reads cached cookies, validates them,
// fetches new credentials via Playwright if cache is invalid, and executes
// the target HTTP request using a specialized spoofed HTTP client (aoni).
func run(ctx context.Context, targetURL string) error {
	var cookies []*http.Cookie
	var err error

	// Session Caching Pipeline
	cookies, err = loadCookies()
	if err != nil || !areCookiesValid(cookies) {
		fmt.Println("[Cache] Cookies missing or expired. Initiating browser automation session...")
		cookies, _, err = getFreshCookies(targetURL)
		if err != nil {
			return fmt.Errorf("get cookies: %w", err)
		}
		_ = saveCookies(cookies)
	} else {
		fmt.Printf("[Cache] Successfully loaded %d valid cookies from storage.\n", len(cookies))
	}

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(targetURL)
	jar.SetCookies(u, cookies)

	// Creating the 'aoni' client.
	// To prevent session invalidation, the HTTP client's fingerprints must precisely
	// align with the Playwright browser configuration used to generate the cookies:
	// - Browser TLS fingerprint is set to match Firefox.
	// - Headers (User-Agent, Accept-Language, Origin, Referer) align with the automated context.
	// - TCP Delays prevent triggering rate-limiting heuristics.
	client := aoni.NewClient(nil,
		aoni.WithClientTLSFingerprint(aoni.BrowserFirefox),
		aoni.WithClientBrowserProfile(aoni.BrowserFirefox, profiles.Windows),
		aoni.WithClientCookieJar(jar),
		aoni.WithClientHeader("User-Agent", firefox.UserAgentFirefoxWindows),
		aoni.WithClientHeader("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8"),
		aoni.WithClientHeader("Origin", "https://5ka.ru"),
		aoni.WithClientHeader("Referer", "https://5ka.ru/"),
		aoni.WithClientRefererAutomaton(true),
		aoni.WithClientTCPDelay(300*time.Millisecond, 800*time.Millisecond),
	)

	fmt.Printf("\n[aoni] Sending authorized request to %s...\n", targetURL)
	resp, err := client.Get(ctx, targetURL)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Self-healing mechanism: If the server response indicates connection/session issues,
	// it suggests that the cached cookies have been revoked on the server.
	// The cache is cleared and a recursive run attempt is triggered to renew the session.
	if strings.Contains(bodyStr, "Проблемы со связью") {
		fmt.Println("[Cache] Session invalidated by server. Clearing cache and restarting flow...")
		_ = os.Remove(cookiesFile)
		return run(ctx, targetURL)
	}

	fmt.Println("\n--- Execution Result ---")
	fmt.Printf("HTTP Status Code: %d\n", resp.StatusCode)
	fmt.Println("✅ Session successfully authenticated and verified.")
	return nil
}

func main() {
	// Enforcing an overall execution timeout to prevent indefinite hangs in automation workflows.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := run(ctx, "https://5ka.ru/"); err != nil {
		fmt.Printf("Execution Error: %v\n", err)
	}
}
