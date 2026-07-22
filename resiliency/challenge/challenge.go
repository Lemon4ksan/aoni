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
	"strings"

	"github.com/lemon4ksan/aoni"
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

// DetectCloudflareChallenge inspects the response headers and buffered HTML prefix
// to determine if Cloudflare JS/WAF challenge page was returned.
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

	bodyStr := strings.ToLower(string(prefix))
	if containsCloudflareSignatures(bodyStr) {
		return true, ErrCloudflareDetected
	}

	return false, nil
}

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

func hasHTMLTags(prefix []byte) bool {
	lower := strings.ToLower(string(prefix))
	return strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype html")
}

func containsCloudflareSignatures(body string) bool {
	return strings.Contains(body, "cf-challenge") ||
		strings.Contains(body, "ray id") ||
		strings.Contains(body, "cloudflare")
}
