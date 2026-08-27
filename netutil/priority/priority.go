// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package priority implements RFC 9218 Extensible Priorities for HTTP/2, HTTP/3, and HTTP/1.1
// aligning request resource scheduling with modern Chromium architecture.
package priority

import (
	"fmt"
	"strconv"
	"strings"
)

// HeaderPriority is the standard RFC 9218 header name for extensible HTTP priorities.
const HeaderPriority = "priority"

// Standard RFC 9218 urgency levels (0 to 7).
const (
	// UrgencyUserBlocking (0) represents critical blocking resources (e.g. HTML documents, synchronous scripts).
	UrgencyUserBlocking = 0

	// UrgencyHigh (1) represents high-priority interactive resources (e.g. API fetch calls, XHR).
	UrgencyHigh = 1

	// UrgencyMedium (2) represents normal sub-resources.
	UrgencyMedium = 2

	// UrgencyLow (3) represents lower priority visual assets (e.g. images, icons).
	UrgencyLow = 3

	// UrgencyVeryLow (4) represents non-blocking assets.
	UrgencyVeryLow = 4

	// UrgencyBackground (5) represents background operations (e.g. prefetch, telemetry, analytics).
	UrgencyBackground = 5

	// UrgencyIdle (6) represents speculative or maintenance tasks.
	UrgencyIdle = 6

	// UrgencyMinimum (7) represents the lowest possible priority.
	UrgencyMinimum = 7
)

// Standard predefined [Priority] configurations.
var (
	// UserBlocking represents urgency 0 non-incremental priority (HTML / critical).
	UserBlocking = Priority{Urgency: UrgencyUserBlocking, Incremental: false}

	// Interactive represents urgency 1 incremental priority (API / fetch).
	Interactive = Priority{Urgency: UrgencyHigh, Incremental: true}

	// Low represents urgency 3 incremental priority (images / media).
	Low = Priority{Urgency: UrgencyLow, Incremental: true}

	// Background represents urgency 5 non-incremental priority (telemetry / prefetch).
	Background = Priority{Urgency: UrgencyBackground, Incremental: false}
)

// Priority encapsulates RFC 9218 priority parameters: urgency (0-7) and incremental streaming flag.
type Priority struct {
	// Urgency indicates resource priority level from 0 (highest) to 7 (lowest).
	Urgency int

	// Incremental indicates whether the response can be processed incrementally in parallel.
	Incremental bool
}

// New constructs a validated [Priority]. Caps urgency to [0, 7].
func New(urgency int, incremental bool) Priority {
	if urgency < 0 {
		urgency = 0
	} else if urgency > 7 {
		urgency = 7
	}

	return Priority{
		Urgency:     urgency,
		Incremental: incremental,
	}
}

// Format serializes the [Priority] into an RFC 9218 Structured Field dictionary string (e.g. "u=1, i").
func (p Priority) Format() string {
	urg := p.Urgency
	if urg < 0 {
		urg = 0
	} else if urg > 7 {
		urg = 7
	}

	if p.Incremental {
		return fmt.Sprintf("u=%d, i", urg)
	}

	return fmt.Sprintf("u=%d", urg)
}

func (p Priority) String() string {
	return p.Format()
}

// Parse deserializes an RFC 9218 priority header string (e.g. "u=1, i", "u=3", "i, u=0").
func Parse(s string) (Priority, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Priority{Urgency: 3, Incremental: false}, nil
	}

	p := Priority{Urgency: 3, Incremental: false}
	parts := strings.Split(s, ",")

	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "i" || item == "i=?1" {
			p.Incremental = true
			continue
		}

		if item == "i=?0" {
			p.Incremental = false
			continue
		}

		if strings.HasPrefix(item, "u=") {
			valStr := strings.TrimPrefix(item, "u=")

			val, err := strconv.Atoi(strings.TrimSpace(valStr))
			if err == nil && val >= 0 && val <= 7 {
				p.Urgency = val
			}
		}
	}

	return p, nil
}
