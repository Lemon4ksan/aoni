// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package challenge provides automated detection and solving hooks for WAF/DDoS interstitial challenge pages
// (e.g. Cloudflare Turnstile, JavaScript challenges, or CAPTCHAs).
//
// # Architectural Philosophy: Transparent Challenge Resolution
//
// When anti-bot systems intercept requests with challenge HTML payloads, standard HTTP clients fail or return 403 Forbidden.
// The challenge subsystem non-destructively inspects buffered response prefixes and passes detected challenges
// to pluggable [Solver] implementations to retrieve clearance cookies or solve proofs without breaking client code.
//
// # Example
//
//	client := aoni.New(
//	    option.WithChallengeSolver(myTurnstileSolver),
//	)
package challenge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// Detector inspects an HTTP response and returns whether it constitutes an anti-bot challenge page.
type Detector func(resp *http.Response) (bool, error)

// Solver provides an automated driver for solving anti-bot challenge pages and retrying the request.
type Solver interface {
	// Solve resolves a WAF challenge response and retries the request.
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

// prefixProvider allows inspecting the pre-buffered byte prefix of a response body
// without consuming the remaining underlying reader stream.
type prefixProvider interface {
	BufferedPrefix() []byte
}

// ErrCloudflareDetected indicates a Cloudflare JS challenge or
// CAPTCHA was detected in the response body.
var ErrCloudflareDetected = errors.New("aoni: cloudflare challenge detected")

// DefaultDetector serves as the standard detector used across the pipeline.
var DefaultDetector Detector = DetectCloudflareChallenge

// DetectCloudflareChallenge inspects response status codes, headers, and buffered HTML body prefixes
// to determine whether a Cloudflare JavaScript or CAPTCHA challenge page was returned.
//
// Black-Box Behavior:
// Performs non-destructive inspection of up to 4096 bytes of the response body prefix (using pre-buffered fast paths when available).
//
// Preconditions:
//   - If resp or resp.Body are nil, returns (false, nil).
//
// Postconditions:
//   - Returns (true, ErrCloudflareDetected) if Cloudflare JS/CAPTCHA signatures are present; otherwise returns (false, nil).
//   - Leaves the response body readable for subsequent downstream handlers without corrupting stream state.
func DetectCloudflareChallenge(resp *http.Response) (bool, error) {
	if resp == nil || resp.Body == nil {
		return false, nil
	}

	prefix, err := extractBodyPrefix(resp, 4096)
	if err != nil || len(prefix) == 0 {
		return false, nil //nolint:nilerr
	}

	if !isHTMLContentType(resp.Header.Get(header.ContentType)) && !hasHTMLTags(prefix) {
		return false, nil
	}

	if containsCloudflareSignatures(prefix) {
		return true, ErrCloudflareDetected
	}

	return false, nil
}

// extractBodyPrefix retrieves initial body bytes without exhausting the stream.
func extractBodyPrefix(resp *http.Response, fallbackLimit int64) ([]byte, error) {
	// Fast path: body is already buffered by aoni pipeline.
	if provider, ok := resp.Body.(prefixProvider); ok {
		return provider.BufferedPrefix(), nil
	}

	// Fallback path: plain response body (e.g. in unit tests or standalone usage).
	buf, err := io.ReadAll(io.LimitReader(resp.Body, fallbackLimit))
	if err != nil {
		return nil, err
	}

	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(buf), resp.Body),
		Closer: resp.Body,
	}

	return buf, nil
}

// isHTMLContentType reports whether contentType header signifies HTML markup.
func isHTMLContentType(contentType string) bool {
	if contentType == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	return bytesconv.EqualFoldASCII(mediaType, header.MIMETextHTML) ||
		bytesconv.EqualFoldASCII(mediaType, "application/xhtml+xml")
}

// hasHTMLTags checks whether prefix starts with common HTML doctype or root tags.
func hasHTMLTags(prefix []byte) bool {
	return bytesconv.ContainsFoldASCII(prefix, "<html") ||
		bytesconv.ContainsFoldASCII(prefix, "<!doctype html")
}

// containsCloudflareSignatures checks whether prefix contains Cloudflare Challenge or CAPTCHA indicators.
func containsCloudflareSignatures(prefix []byte) bool {
	return bytesconv.ContainsFoldASCII(prefix, "cf-challenge") ||
		bytesconv.ContainsFoldASCII(prefix, "ray id") ||
		bytesconv.ContainsFoldASCII(prefix, "cloudflare")
}
