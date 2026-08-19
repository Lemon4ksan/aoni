// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ca

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// OIDCConfig specifies OIDC token validation rules and claim extraction settings.
type OIDCConfig struct {
	Issuer    string
	ClientID  string
	ClaimName string
}

// IssueFromOIDCToken parses and validates an OIDC JWT ID token, extracts the user claim,
// and issues a short-lived user SSH Certificate.
//
// Zero-Dependency JWT Parsing:
// Parses unencrypted JWT claims directly using standard base64 and JSON,
// making it fast and lightweight.
func (c *CA) IssueFromOIDCToken(
	_ context.Context,
	rawIDToken string,
	userKey ssh.PublicKey,
	oidcCfg OIDCConfig,
	opts ...IssueOption,
) (*ssh.Certificate, string, error) {
	if rawIDToken == "" {
		return nil, "", ErrInvalidOIDCToken
	}

	claims, err := parseUnverifiedJWT(rawIDToken)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrInvalidOIDCToken, err)
	}

	if err := validateOIDCClaims(claims, oidcCfg); err != nil {
		return nil, "", err
	}

	username, err := extractUserClaim(claims, oidcCfg.ClaimName)
	if err != nil {
		return nil, "", err
	}

	cert, err := c.IssueUserCert(userKey, username, opts...)
	if err != nil {
		return nil, "", err
	}

	return cert, username, nil
}

func parseUnverifiedJWT(tokenStr string) (map[string]any, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidOIDCToken
	}

	payloadSegment := parts[1]
	if l := len(payloadSegment) % 4; l > 0 {
		payloadSegment += strings.Repeat("=", 4-l)
	}

	payloadBytes, err := base64.URLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return nil, ErrInvalidOIDCToken
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrInvalidOIDCToken
	}

	return claims, nil
}

func validateOIDCClaims(claims map[string]any, cfg OIDCConfig) error {
	if expVal, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(expVal) {
			return ErrCertExpired
		}
	}

	if cfg.Issuer != "" {
		if issVal, ok := claims["iss"].(string); !ok || !strings.HasPrefix(issVal, cfg.Issuer) {
			return fmt.Errorf("%w: issuer mismatch", ErrInvalidOIDCToken)
		}
	}

	return nil
}

func extractUserClaim(claims map[string]any, claimName string) (string, error) {
	targetClaim := claimName
	if targetClaim == "" {
		targetClaim = "email"
	}

	val, ok := claims[targetClaim].(string)
	if !ok || val == "" {
		if targetClaim == "email" {
			if subVal, ok := claims["sub"].(string); ok && subVal != "" {
				return subVal, nil
			}
		}

		return "", fmt.Errorf("%w: claim %q", ErrMissingOIDCClaim, targetClaim)
	}

	return val, nil
}
