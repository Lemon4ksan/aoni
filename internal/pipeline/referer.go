// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"net/url"
	"strings"
	"sync"
)

// RefererPolicy defines cross-origin Referer header stripping behavior (RFC 9110 §10.1.3).
type RefererPolicy int

const (
	// PolicyStrictOriginWhenCrossOrigin sends full URL for same-origin, origin only for cross-origin, none for downgrade.
	PolicyStrictOriginWhenCrossOrigin RefererPolicy = iota
	// PolicyNoRefererWhenDowngrade sends full URL except when downgrading HTTPS -> HTTP.
	PolicyNoRefererWhenDowngrade
	// PolicyNoReferer never sends Referer header.
	PolicyNoReferer
	// PolicyUnsafeURL always sends full URL regardless of origin or protocol.
	PolicyUnsafeURL
)

// RefererAutomaton tracks and calculates Referer headers across sequential requests.
type RefererAutomaton struct {
	mu      sync.RWMutex
	lastURL *url.URL
	policy  RefererPolicy
}

// NewRefererAutomaton constructs a [RefererAutomaton].
func NewRefererAutomaton(policy RefererPolicy) *RefererAutomaton {
	return &RefererAutomaton{policy: policy}
}

// ComputeReferer calculates the appropriate Referer header value for an upcoming request to targetURL.
func (a *RefererAutomaton) ComputeReferer(targetURL *url.URL) string {
	if a == nil || targetURL == nil {
		return ""
	}

	a.mu.RLock()
	last := a.lastURL
	policy := a.policy
	a.mu.RUnlock()

	if last == nil {
		return ""
	}

	if policy == PolicyNoReferer {
		return ""
	}

	isDowngrade := last.Scheme == "https" && targetURL.Scheme == "http"
	if isDowngrade && (policy == PolicyNoRefererWhenDowngrade || policy == PolicyStrictOriginWhenCrossOrigin) {
		return ""
	}

	if policy == PolicyUnsafeURL {
		return last.String()
	}

	sameOrigin := strings.EqualFold(last.Scheme, targetURL.Scheme) &&
		strings.EqualFold(last.Host, targetURL.Host)

	if sameOrigin {
		return last.String()
	}

	if policy == PolicyStrictOriginWhenCrossOrigin {
		return last.Scheme + "://" + last.Host + "/"
	}

	return last.String()
}

// UpdateLastURL updates the state automaton with the completed request URL.
func (a *RefererAutomaton) UpdateLastURL(completedURL *url.URL) {
	if a == nil || completedURL == nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.lastURL = completedURL
}
