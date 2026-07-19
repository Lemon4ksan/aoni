// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This protection is quite straightforward. Anubis operates as a reverse proxy.
// When accessing a website, it forces the browser to perform a SHA-256-based proof-of-work cryptographic calculation.
// The real browser performs this calculation in JavaScript in a few seconds, receives a signed JWT token
// in the form of a cookie (techaro.lol-anubis), and seamlessly redirects you to the target page.
//
// Simple scripts without JavaScript support cannot perform this calculation and are blocked at the verification stage.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/generic"
)

var (
	rxAnubis        = regexp.MustCompile(`<script id="anubis_challenge"[^>]*>([\s\S]*?)</script>`)
	rxChallengePath = regexp.MustCompile(`/[^"'\s]*pass-challenge`)
)

type AnubisRules struct {
	Difficulty int `json:"difficulty"`
}

type AnubisChallenge struct {
	Rules     AnubisRules `json:"rules"`
	Challenge any         `json:"challenge"`
	ID        string      `json:"id"`
}

func SolveAnubisChallenge(randomData string, difficulty int) (string, string, int64) {
	start := time.Now()
	prefix := strings.Repeat("0", difficulty)

	var nonce uint64
	for {
		nonceStr := strconv.FormatUint(nonce, 10)
		data := randomData + nonceStr

		hash := sha256.Sum256([]byte(data))
		hashHex := hex.EncodeToString(hash[:])

		if strings.HasPrefix(hashHex, prefix) {
			elapsed := time.Since(start).Milliseconds()
			return nonceStr, hashHex, elapsed
		}

		nonce++
	}
}

func parseAnubisChallenge(html string) (randomData, id string, difficulty int, err error) {
	matches := rxAnubis.FindStringSubmatch(html)
	if len(matches) < 2 {
		return "", "", 0, errors.New("anubis challenge script not found")
	}

	var payload AnubisChallenge
	if err := json.Unmarshal([]byte(matches[1]), &payload); err != nil {
		return "", "", 0, err
	}

	switch v := payload.Challenge.(type) {
	case string:
		randomData = v
		id = payload.ID
	case map[string]any:
		if rd, ok := v["randomData"].(string); ok {
			randomData = rd
		}
		if cid, ok := v["id"].(string); ok {
			id = cid
		}
	default:
		return "", "", 0, errors.New("unsupported challenge payload format")
	}

	if randomData == "" || id == "" {
		return "", "", 0, errors.New("failed to extract challenge parameters")
	}

	return randomData, id, generic.Coalesce(payload.Rules.Difficulty, 4), nil
}

func findPassChallengePath(html string) string {
	match := rxChallengePath.FindString(html)
	return generic.Coalesce(match, "/.within.website/x/cmd/anubis/api/pass-challenge")
}

func run(ctx context.Context, targetURL string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}

	client := aoni.NewClient(nil,
		aoni.WithClientTLSFingerprint(aoni.BrowserChrome),
		aoni.WithClientCookieJar(jar),
	)

	resp, err := client.Get(ctx, targetURL)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	htmlContent := string(bodyBytes)

	if strings.Contains(htmlContent, "Making sure you're not a bot!") || strings.Contains(htmlContent, "anubis") {
		fmt.Println("Anubis detected! Initiating waf bypass...")

		randomData, id, difficulty, err := parseAnubisChallenge(htmlContent)
		if err != nil {
			return fmt.Errorf("parse error: %w", err)
		}
		fmt.Printf("Recieved params: ID: %s, Salt: %s, Difficulty: %d\n", id, randomData[:15]+"...", difficulty)

		apiPath := findPassChallengePath(htmlContent)
		fmt.Printf("Verification API path found: %s\n", apiPath)

		nonce, hashHex, elapsed := SolveAnubisChallenge(randomData, difficulty)
		fmt.Printf("Task completed! Nonce: %s, Hash: %s (took: %d ms)\n", nonce, hashHex[:20]+"...", elapsed)

		u, _ := url.Parse(targetURL)
		verificationURL := fmt.Sprintf(
			"https://%s%s?response=%s&nonce=%s&id=%s&redir=%s&elapsedTime=%d",
			u.Host,
			apiPath,
			hashHex,
			nonce,
			id,
			url.QueryEscape(u.Path),
			elapsed,
		)

		fmt.Println("Sending verification response...")
		verifyResp, err := client.Get(ctx, verificationURL)
		if err != nil {
			return fmt.Errorf("verification error: %w", err)
		}
		verifyResp.Body.Close()

		fmt.Println("Retrying initial request...")
		finalResp, err := client.Get(ctx, targetURL)
		if err != nil {
			panic(err)
		}
		defer finalResp.Body.Close()

		finalBody, err := io.ReadAll(finalResp.Body)
		if err != nil {
			panic(err)
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(finalBody))
		if err != nil {
			panic(err)
		}

		fmt.Println("\n--- Operation Successful! ---")
		fmt.Printf("Final page header: %s\n", doc.Find("#firstHeading").Text())
		fmt.Printf("Response code: %d\n", finalResp.StatusCode)

	} else {
		doc, _ := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
		fmt.Printf("Page loaded without challenge. Header: %s\n", doc.Find("#firstHeading").Text())
	}

	return nil
}

func main() {
	target := "https://developer.valvesoftware.com/wiki/Source_SDK_Base_2013"

	if err := run(context.Background(), target); err != nil {
		panic(err)
	}
}
