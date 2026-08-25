// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"net"
	"net/url"
	"strings"
)

// ValidateChallenge verifies that challenge conforms to RFC 9577 §2.1.3 validation requirements:
//   - Token type is supported and recognized.
//   - Challenge structure is well-formed (redemption context is 0 or 32 bytes).
//   - If origin_info is non-empty, the challenging origin hostname is present (case-insensitive check).
func ValidateChallenge(originHost string, c *TokenChallenge) error {
	if c == nil {
		return ErrInvalidChallengeData
	}

	if !IsSupportedTokenType(c.TokenType) {
		return ErrUnsupportedTokenType
	}

	if len(c.RedemptionContext) != 0 && len(c.RedemptionContext) != 32 {
		return ErrInvalidRedemptionContext
	}

	if c.OriginInfo != "" && originHost != "" {
		origins := strings.Split(c.OriginInfo, ",")
		matched := false

		for _, o := range origins {
			if matchOrigin(o, originHost) {
				matched = true
				break
			}
		}

		if !matched {
			return ErrOriginMismatch
		}
	}

	return nil
}

// IsSupportedTokenType reports whether tokenType is recognized and supported.
func IsSupportedTokenType(t TokenType) bool {
	switch t {
	case TypeBlindRSA, TypePubliclyVerifiable:
		return true
	default:
		return false
	}
}

func matchOrigin(originA, originB string) bool {
	hostA := normalizeHost(originA)
	hostB := normalizeHost(originB)

	if strings.EqualFold(hostA, hostB) {
		return true
	}

	cleanA := strings.TrimSuffix(hostA, ":443")
	cleanB := strings.TrimSuffix(hostB, ":443")

	if strings.EqualFold(cleanA, cleanB) {
		return true
	}

	// Compare base hostnames if one side does not specify a port
	hA, _, errA := net.SplitHostPort(hostA)
	if errA != nil {
		hA = hostA
	}

	hB, _, errB := net.SplitHostPort(hostB)
	if errB != nil {
		hB = hostB
	}

	return strings.EqualFold(hA, hB)
}

func normalizeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			return u.Host
		}
	}

	return raw
}
