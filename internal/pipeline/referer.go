// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
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

type refererState struct {
	lastURL *url.URL
	policy  RefererPolicy
}

// RefererAutomaton tracks and calculates Referer headers across sequential requests.
type RefererAutomaton struct {
	state generic.Safe[refererState]
}

// NewRefererAutomaton constructs a [RefererAutomaton].
func NewRefererAutomaton(policy RefererPolicy) *RefererAutomaton {
	return &RefererAutomaton{
		state: *generic.NewSafe(refererState{policy: policy}),
	}
}

// ComputeReferer calculates the appropriate Referer header value for an upcoming request to targetURL.
func (a *RefererAutomaton) ComputeReferer(targetURL *url.URL) string {
	if a == nil || targetURL == nil {
		return ""
	}

	st := a.state.Get()
	last := st.lastURL
	policy := st.policy

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
		totalLen := len(last.Scheme) + 4 + len(last.Host)
		if totalLen <= 128 {
			var buf [128]byte

			n := copy(buf[:], last.Scheme)
			copy(buf[n:], "://")
			n += 3
			n += copy(buf[n:], last.Host)
			buf[n] = '/'

			return string(buf[:totalLen])
		}

		return last.Scheme + "://" + last.Host + "/"
	}

	return last.String()
}

// UpdateLastURL updates the state automaton with the completed request URL.
func (a *RefererAutomaton) UpdateLastURL(completedURL *url.URL) {
	if a == nil || completedURL == nil {
		return
	}

	a.state.Mutate(func(st *refererState) {
		st.lastURL = completedURL
	})
}
