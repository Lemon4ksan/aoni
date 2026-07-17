// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
)

// DefaultChallengeDetector is the default detector used by Client.
var DefaultChallengeDetector ChallengeDetector = DetectCloudflareChallenge

// ChallengeSolver defines a standard interface to delegate WAF challenge
// solving to an external driver (like Playwright, Puppeteer, or Selenium).
type ChallengeSolver interface {
	Solve(ctx context.Context, err error, req *http.Request) (*http.Response, error)
}

// ChallengeDetector is a function type that decides whether a response represents a WAF challenge.
// It returns true and the associated error if a challenge is detected.
type ChallengeDetector func(resp *http.Response) (bool, error)

// WithChallengeSolver returns a clone of c configured with the specified ChallengeSolver.
func (c *Client) WithChallengeSolver(solver ChallengeSolver) *Client {
	newClient := c.Clone()
	newClient.defaults.ChallengeSolver = solver
	newClient.rebuildChain()

	return newClient
}

// DetectCloudflareChallenge checks if the response is a Cloudflare WAF/JS challenge.
func DetectCloudflareChallenge(resp *http.Response) (bool, error) {
	if resp == nil {
		return false, nil
	}

	contentType := resp.Header.Get("Content-Type")

	isHTMLType := false
	if contentType != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
				isHTMLType = true
			}
		}
	}

	if resp.Body != nil {
		bodyBytes, err := readAndReplaceBody(resp, 100*1024)
		if err != nil || len(bodyBytes) == 0 {
			return false, nil //nolint:nilerr
		}

		bodyStr := string(bodyBytes)
		lowerBody := strings.ToLower(bodyStr)
		isHTML := isHTMLType || strings.Contains(lowerBody, "<html") || strings.Contains(lowerBody, "<!doctype html")

		if isHTML {
			if strings.Contains(bodyStr, "cf-challenge") || strings.Contains(bodyStr, "ray id") ||
				strings.Contains(bodyStr, "cloudflare") {
				return true, ErrCloudflareChallenge
			}
		}
	}

	return false, nil
}

// readAndReplaceBody reads up to limit bytes from resp.Body and replaces the body
// with a new MultiReader to allow reading the body again from the start.
func readAndReplaceBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp.Body == nil {
		return nil, nil
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}

	resp.Body = &readCloserWithBytes{
		Reader: io.MultiReader(bytes.NewReader(bodyBytes), resp.Body),
		closer: resp.Body,
	}

	return bodyBytes, nil
}

type readCloserWithBytes struct {
	io.Reader
	closer io.Closer
}

func (r *readCloserWithBytes) Close() error {
	return r.closer.Close()
}
