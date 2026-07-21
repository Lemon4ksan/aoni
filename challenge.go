// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"mime"
	"net/http"
	"strings"

	"github.com/lemon4ksan/aoni/internal/io"
)

// DefaultChallengeDetector is the default detector used by Client.
var DefaultChallengeDetector = DetectCloudflareChallenge

// ChallengeSolver defines a standard interface to delegate WAF challenge
// solving to an external driver (like Playwright, Puppeteer, or Selenium).
type ChallengeSolver interface {
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

// ChallengeDetector is a function type that decides whether a response represents a WAF challenge.
// It returns true and the associated error if a challenge is detected.
type ChallengeDetector func(resp *http.Response) (bool, error)

// DetectCloudflareChallenge checks if the response is a Cloudflare WAF/JS challenge.
func DetectCloudflareChallenge(resp *http.Response) (bool, error) {
	if resp == nil || resp.Body == nil {
		return false, nil
	}

	isHTMLType := false
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
				isHTMLType = true
			}
		}
	}

	// We expect resp.Body to be already wrapped in *ExplicitBufferedBody.
	// If it is, we inspect the Prefix. If not, we don't inspect.
	buffered, ok := resp.Body.(*io.ExplicitBufferedBody)
	if !ok || len(buffered.Prefix) == 0 {
		return false, nil
	}

	bodyStr := string(buffered.Prefix)
	lowerBody := strings.ToLower(bodyStr)
	isHTML := isHTMLType || strings.Contains(lowerBody, "<html") || strings.Contains(lowerBody, "<!doctype html")

	if isHTML {
		if strings.Contains(bodyStr, "cf-challenge") || strings.Contains(bodyStr, "ray id") ||
			strings.Contains(bodyStr, "cloudflare") {
			return true, ErrCloudflareChallenge
		}
	}

	return false, nil
}
