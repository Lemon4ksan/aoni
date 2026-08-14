// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package challenge provides WAF/DDoS challenge detection and automated solving integration.
package challenge

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// prefixProvider allows inspecting the pre-buffered byte prefix of a response body
// without consuming the remaining underlying reader stream.
type prefixProvider interface {
	BufferedPrefix() []byte
}

// ErrCloudflareDetected indicates a Cloudflare JS challenge or
// CAPTCHA was detected in the response body.
var ErrCloudflareDetected = errors.New("aoni: cloudflare challenge detected")

// DefaultDetector serves as the standard detector used across the pipeline.
var DefaultDetector aoni.ChallengeDetector = DetectCloudflareChallenge

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

	if !isHTMLContentType(resp.Header.Get("Content-Type")) && !hasHTMLTags(prefix) {
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

	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
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
