// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/option"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
	"github.com/mxschmitt/playwright-go"
)

var protectionCookies = []string{
	"datadome",     // DataDome
	"ak_bmsc",      // Akamai
	"bm_sz",        // Akamai
	"cf_clearance", // Cloudflare
	"__cf_bm",      // Cloudflare Bot Management
	"_pxhd",        // PerimeterX
	"_px",          // PerimeterX
}

func getProtectionCookie(targetURL string) (name, value string, err error) {
	pw, err := playwright.Run()
	if err != nil {
		return "", "", fmt.Errorf("playwright run: %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: new(false),
		Args: []string{
			"--disable-blink-features=AutomationControlled",
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("launch browser: %w", err)
	}
	defer browser.Close()

	browserContext, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: new(chrome.UserAgentWindows),
	})
	if err != nil {
		return "", "", fmt.Errorf("create context: %w", err)
	}
	defer browserContext.Close()

	page, err := browserContext.NewPage()
	if err != nil {
		return "", "", fmt.Errorf("create page: %w", err)
	}

	fmt.Printf("[Playwright] Opening page: %s\n", targetURL)
	if _, err = page.Goto(targetURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateLoad,
		Timeout:   playwright.Float(60000),
	}); err != nil {
		return "", "", fmt.Errorf("goto: %w", err)
	}

	time.Sleep(2 * time.Second)

	fmt.Println("[Playwright] Waiting for cookie (up to 60 seconds)...")
	start := time.Now()

	for time.Since(start) <= 60*time.Second {
		cookies, err := browserContext.Cookies()
		if err != nil {
			return "", "", fmt.Errorf("get cookies: %w", err)
		}

		for _, cookie := range cookies {
			for _, pName := range protectionCookies {
				if cookie.Name == pName {
					fmt.Printf("[Playwright] Cookie found %s: %s...\n", pName, cookie.Value[:15])
					return pName, cookie.Value, nil
				}
			}
		}

		// If we found at least one cookie, but not from the list, we can print them for debugging.
		// For now, we're just waiting.

		time.Sleep(500 * time.Millisecond)
	}

	fmt.Println("[Playwright] Cookies for page:")
	cookies, _ := browserContext.Cookies()
	for _, c := range cookies {
		fmt.Printf("  %s = %s (domain: %s)\n", c.Name, c.Value, c.Domain)
	}

	return "", "", errors.New("cookie wasn't found in 60 секунд")
}

func buildCookieDomain(u *url.URL) string {
	parts := strings.Split(u.Host, ".")
	if len(parts) > 2 {
		return "." + strings.Join(parts[len(parts)-2:], ".")
	}
	return u.Host
}

func run(ctx context.Context, targetURL string) error {
	cookieName, cookieValue, err := getProtectionCookie(targetURL)
	if err != nil {
		return fmt.Errorf("get protection cookie: %w", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		return err
	}

	jar.SetCookies(u, []*http.Cookie{
		{
			Name:     cookieName,
			Value:    cookieValue,
			Domain:   buildCookieDomain(u),
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
		},
	})

	client := aoni.NewClient(nil,
		option.WithChrome(),
		option.WithCookieJar(jar),
		option.WithRefererAutomaton(true),
		option.WithTCPDelay(150*time.Millisecond, 400*time.Millisecond),
	)

	resp, err := client.Get(ctx, targetURL)
	if err != nil {
		return fmt.Errorf("aoni request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	fmt.Println("\n--- Result ---")
	fmt.Printf("Response code: %d\n", resp.StatusCode)
	title := doc.Find("title").Text()
	fmt.Printf("Page title: %s\n", strings.TrimSpace(title))

	if resp.StatusCode == 200 && !strings.Contains(string(bodyBytes), "Just a moment") {
		fmt.Println("✅ Evasion successful!")
	} else {
		fmt.Println("⚠️ The page may still be locked. Check output.")
	}

	return nil
}

func main() {
	target := "https://www.tripadvisor.com/"

	if err := run(context.Background(), target); err != nil {
		panic(err)
	}
}
