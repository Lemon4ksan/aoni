// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// rsaChallengeB64 is the exact unpadded base64url representation of the 25-byte Vector 1 TokenChallenge
// (0002 0012 "issuer.example.com" 00 0000).
// 25 bytes encodes into exactly 34 base64 characters ending in "AAAA".
const rsaChallengeB64 = "AAIAEmlzc3Vlci5leGFtcGxlLmNvbQAAAA"

func TestParseChallenge_RFC9578Vectors(t *testing.T) {
	t.Parallel()

	t.Run("Vector1_BlindRSA_Minimal", func(t *testing.T) {
		t.Parallel()

		header := `PrivateToken challenge="` + rsaChallengeB64 + `"`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc.TokenType != TokenTypeBlindRSA {
			t.Errorf("expected TokenType 0x%04x, got 0x%04x", TokenTypeBlindRSA, tc.TokenType)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("expected IssuerName issuer.example.com, got %s", tc.IssuerName)
		}

		if len(tc.RedemptionContext) != 0 {
			t.Errorf("expected empty RedemptionContext, got %d", len(tc.RedemptionContext))
		}

		if tc.OriginInfo != "" {
			t.Errorf("expected empty OriginInfo, got %s", tc.OriginInfo)
		}

		rawBytes, err := DecodeBase64URL(rsaChallengeB64)
		if err != nil {
			t.Fatalf("DecodeBase64URL failed: %v", err)
		}

		expectedDigest := sha256.Sum256(rawBytes)
		if tc.Digest != expectedDigest {
			t.Errorf("expected Digest %x, got %x", expectedDigest, tc.Digest)
		}
	})

	t.Run("Vector2_ContextBoundAndOriginInfo", func(t *testing.T) {
		t.Parallel()

		rc := make([]byte, 32)
		for i := range rc {
			rc[i] = byte(i + 1)
		}

		original := &TokenChallenge{
			TokenType:         TokenTypeBlindRSA,
			IssuerName:        "issuer.example.com",
			RedemptionContext: rc,
			OriginInfo:        "https://api.example.com",
		}

		wire, err := MarshalTokenChallenge(original)
		if err != nil {
			t.Fatalf("MarshalTokenChallenge failed: %v", err)
		}

		b64 := EncodeBase64URL(wire)
		header := `PrivateToken challenge="` + b64 + `"`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc.TokenType != TokenTypeBlindRSA {
			t.Errorf("TokenType mismatch")
		}

		if tc.IssuerName != original.IssuerName {
			t.Errorf("IssuerName mismatch")
		}

		if !bytes.Equal(tc.RedemptionContext, original.RedemptionContext) {
			t.Errorf("RedemptionContext mismatch")
		}

		if tc.OriginInfo != original.OriginInfo {
			t.Errorf("OriginInfo mismatch")
		}
	})
}

func TestParseChallenge_MultipleChallenges(t *testing.T) {
	t.Parallel()

	// Vector 1 (RSA) & Vector 3 (VOPRF) in one header
	rsaChallengeHex := "000200126973737565722e6578616d706c652e636f6d000000"
	rsaB64 := EncodeBase64URL(mustHexDecode(t, rsaChallengeHex))

	voprfChallengeHex := "00010014766f7072662e636c6f7564666c6172652e636f6d000000"
	voprfB64 := EncodeBase64URL(mustHexDecode(t, voprfChallengeHex))

	header := `PrivateToken challenge="` + rsaB64 + `", max-age=300, PrivateToken challenge="` + voprfB64 + `", max-age=3600`

	t.Run("ParseChallenges_ReturnsBoth", func(t *testing.T) {
		t.Parallel()

		challenges, err := ParseChallenges(header)
		if err != nil {
			t.Fatalf("ParseChallenges failed: %v", err)
		}

		if len(challenges) != 2 {
			t.Fatalf("expected 2 challenges, got %d", len(challenges))
		}

		if challenges[0].TokenType != TokenTypeBlindRSA {
			t.Errorf("challenge 0: expected TokenTypeBlindRSA, got 0x%04x", challenges[0].TokenType)
		}

		if challenges[0].MaxAge != 300*time.Second {
			t.Errorf("challenge 0: expected MaxAge 300s, got %v", challenges[0].MaxAge)
		}

		if challenges[1].TokenType != TokenTypeVOPRF {
			t.Errorf("challenge 1: expected TokenTypeVOPRF, got 0x%04x", challenges[1].TokenType)
		}

		if challenges[1].MaxAge != 3600*time.Second {
			t.Errorf("challenge 1: expected MaxAge 3600s, got %v", challenges[1].MaxAge)
		}
	})

	t.Run("ParseChallenge_ReturnsFirst", func(t *testing.T) {
		t.Parallel()

		first, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if first.TokenType != TokenTypeBlindRSA {
			t.Errorf("expected first challenge to be TokenTypeBlindRSA, got 0x%04x", first.TokenType)
		}
	})
}

func TestParseChallenge_MultipleSchemes(t *testing.T) {
	t.Parallel()

	t.Run("BasicBeforePrivateToken", func(t *testing.T) {
		t.Parallel()

		header := `Basic realm="Access to staging", PrivateToken challenge="` + rsaChallengeB64 + `"`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("expected issuer.example.com, got %s", tc.IssuerName)
		}
	})

	t.Run("BearerBeforePrivateToken", func(t *testing.T) {
		t.Parallel()

		header := `Bearer mF_9.B5f-4.1JqM, PrivateToken challenge="` + rsaChallengeB64 + `"`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("expected issuer.example.com, got %s", tc.IssuerName)
		}
	})

	t.Run("CommasInForeignQuotedStrings", func(t *testing.T) {
		t.Parallel()

		header := `Basic realm="Zone 1, Zone 2, Area 51", PrivateToken challenge="` + rsaChallengeB64 + `"`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("expected issuer.example.com, got %s", tc.IssuerName)
		}
	})
}

func TestParseChallenge_ParameterVariations(t *testing.T) {
	t.Parallel()

	t.Run("CaseInsensitiveScheme", func(t *testing.T) {
		t.Parallel()

		cases := []string{
			`privatetoken challenge="` + rsaChallengeB64 + `"`,
			`PRIVATETOKEN challenge="` + rsaChallengeB64 + `"`,
			`PrivateToken challenge="` + rsaChallengeB64 + `"`,
		}
		for _, c := range cases {
			tc, err := ParseChallenge(c)
			if err != nil {
				t.Errorf("failed for %q: %v", c, err)
			} else if tc.IssuerName != "issuer.example.com" {
				t.Errorf("issuer mismatch for %q", c)
			}
		}
	})

	t.Run("CaseInsensitiveParams", func(t *testing.T) {
		t.Parallel()

		header := `PrivateToken CHALLENGE="` + rsaChallengeB64 + `", MAX-AGE=120`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc.MaxAge != 120*time.Second {
			t.Errorf("expected MaxAge 120s, got %v", tc.MaxAge)
		}
	})

	t.Run("WithOptionalTokenKey", func(t *testing.T) {
		t.Parallel()

		dummyKey := []byte("dummy-issuer-subject-public-key-info-der-octets")
		keyB64 := EncodeBase64URL(dummyKey)
		header := `PrivateToken challenge="` + rsaChallengeB64 + `", token-key="` + keyB64 + `"`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if !bytes.Equal(tc.TokenKey, dummyKey) {
			t.Errorf("TokenKey mismatch")
		}

		expectedKeyID := sha256.Sum256(dummyKey)
		if tc.TokenKeyID != expectedKeyID {
			t.Errorf("TokenKeyID mismatch")
		}
	})

	t.Run("WithOptionalTokenKeyIDFallback", func(t *testing.T) {
		t.Parallel()

		var keyID [32]byte
		for i := range keyID {
			keyID[i] = byte(i + 10)
		}

		keyIDB64 := EncodeBase64URL(keyID[:])
		header := `PrivateToken challenge="` + rsaChallengeB64 + `", token-key-id="` + keyIDB64 + `"`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc.TokenKeyID != keyID {
			t.Errorf("TokenKeyID mismatch")
		}
	})

	t.Run("WithOptionalMaxAge", func(t *testing.T) {
		t.Parallel()
		// Valid max-age
		h1 := `PrivateToken challenge="` + rsaChallengeB64 + `", max-age=3600`

		tc1, err := ParseChallenge(h1)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc1.MaxAge != 3600*time.Second {
			t.Errorf("expected 3600s, got %v", tc1.MaxAge)
		}

		// Negative max-age ignored
		h2 := `PrivateToken challenge="` + rsaChallengeB64 + `", max-age=-5`

		tc2, err := ParseChallenge(h2)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc2.MaxAge != 0 {
			t.Errorf("expected 0s for negative, got %v", tc2.MaxAge)
		}

		// Non-integer max-age ignored
		h3 := `PrivateToken challenge="` + rsaChallengeB64 + `", max-age="invalid"`

		tc3, err := ParseChallenge(h3)
		if err != nil {
			t.Fatalf("ParseChallenge failed: %v", err)
		}

		if tc3.MaxAge != 0 {
			t.Errorf("expected 0s for non-int, got %v", tc3.MaxAge)
		}
	})

	t.Run("WithOptionalTokenType", func(t *testing.T) {
		t.Parallel()
		// Matching token-type=2
		h1 := `PrivateToken challenge="` + rsaChallengeB64 + `", token-type=2`

		tc1, err := ParseChallenge(h1)
		if err != nil {
			t.Errorf("unexpected error for matching token-type: %v", err)
		} else if tc1.TokenType != TokenTypeBlindRSA {
			t.Errorf("expected TokenTypeBlindRSA, got 0x%04x", tc1.TokenType)
		}

		// Mismatched token-type=1 for RSA challenge
		h2 := `PrivateToken challenge="` + rsaChallengeB64 + `", token-type=1`

		_, err = ParseChallenge(h2)
		if err == nil {
			t.Error("expected error for mismatched token-type")
		}

		if !errors.Is(err, ErrMalformedChallenge) {
			t.Errorf("expected ErrMalformedChallenge, got %v", err)
		}
	})

	t.Run("WithUnknownParametersIgnored", func(t *testing.T) {
		t.Parallel()

		header := `PrivateToken challenge="` + rsaChallengeB64 + `", ext-vendor="cloudflare", priority=1`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("issuer mismatch")
		}
	})

	t.Run("UnquotedParameterValues", func(t *testing.T) {
		t.Parallel()

		header := `PrivateToken challenge=` + rsaChallengeB64 + `, max-age=300`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("unexpected error for unquoted value: %v", err)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("issuer mismatch")
		}

		if tc.MaxAge != 300*time.Second {
			t.Errorf("expected MaxAge 300s, got %v", tc.MaxAge)
		}
	})

	t.Run("WhitespacePermutations", func(t *testing.T) {
		t.Parallel()

		header := `PrivateToken   challenge = "` + rsaChallengeB64 + `" ,   max-age = 60  `

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tc.MaxAge != 60*time.Second {
			t.Errorf("expected 60s, got %v", tc.MaxAge)
		}
	})
}

func TestParseChallengesFromHeader(t *testing.T) {
	t.Parallel()

	t.Run("MultipleHeaderLines", func(t *testing.T) {
		t.Parallel()

		h := http.Header{}
		h.Add("WWW-Authenticate", `Basic realm="admin"`)
		h.Add("WWW-Authenticate", `PrivateToken challenge="`+rsaChallengeB64+`"`)

		challenges, err := ParseChallengesFromHeader(h)
		if err != nil {
			t.Fatalf("ParseChallengesFromHeader failed: %v", err)
		}

		if len(challenges) != 1 {
			t.Fatalf("expected 1 challenge, got %d", len(challenges))
		}

		if challenges[0].IssuerName != "issuer.example.com" {
			t.Errorf("issuer mismatch")
		}
	})

	t.Run("EmptyHeaderMap", func(t *testing.T) {
		t.Parallel()

		_, err := ParseChallengesFromHeader(nil)
		if !errors.Is(err, ErrNoChallenge) {
			t.Errorf("expected ErrNoChallenge, got %v", err)
		}

		_, err = ParseChallengesFromHeader(http.Header{})
		if !errors.Is(err, ErrNoChallenge) {
			t.Errorf("expected ErrNoChallenge, got %v", err)
		}
	})

	t.Run("NoPrivateTokenInHeaders", func(t *testing.T) {
		t.Parallel()

		h := http.Header{}
		h.Add("WWW-Authenticate", `Basic realm="admin"`)
		h.Add("WWW-Authenticate", `Bearer token="abc"`)

		_, err := ParseChallengesFromHeader(h)
		if !errors.Is(err, ErrNoPrivateTokenChallenge) {
			t.Errorf("expected ErrNoPrivateTokenChallenge, got %v", err)
		}
	})
}

func TestParseChallenge_RobustnessAndBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		header      string
		errContains error
	}{
		{
			name:        "EmptyHeader",
			header:      "",
			errContains: ErrNoChallenge,
		},
		{
			name:        "WhitespaceHeader",
			header:      "   \t\r\n  ",
			errContains: ErrNoChallenge,
		},
		{
			name:        "CommasOnly",
			header:      ", , , , ",
			errContains: ErrNoPrivateTokenChallenge,
		},
		{
			name:        "ForeignSchemeOnly",
			header:      `Basic realm="restricted"`,
			errContains: ErrNoPrivateTokenChallenge,
		},
		{
			name:        "SchemeWithoutParams",
			header:      "PrivateToken",
			errContains: ErrMissingChallengeParam,
		},
		{
			name:        "MissingChallengeParam",
			header:      "PrivateToken max-age=3600",
			errContains: ErrMissingChallengeParam,
		},
		{
			name:        "EmptyChallengeParam",
			header:      `PrivateToken challenge=""`,
			errContains: ErrTruncatedChallenge,
		},
		{
			name:        "CorruptedBase64",
			header:      `PrivateToken challenge="!@#$%^&*()"`,
			errContains: ErrInvalidEncoding,
		},
		{
			name:        "TruncatedBinaryInBase64",
			header:      `PrivateToken challenge="AAAA"`, // decodes to 3 bytes
			errContains: ErrTruncatedChallenge,
		},
		{
			name: "InvalidRedemptionContextInBase64",
			// Wire with 1-byte redemption context: 0002000369737301AA0000
			header:      `PrivateToken challenge="` + EncodeBase64URL(mustHexDecode(t, "0002000369737301aa0000")) + `"`,
			errContains: ErrInvalidRedemptionContextLength,
		},
		{
			name: "TrailingGarbageInBase64",
			// Valid 25-byte challenge + 4 bytes deadbeef
			header: `PrivateToken challenge="` + EncodeBase64URL(
				append(mustHexDecode(t, "000200126973737565722e6578616d706c652e636f6d000000"), 0xDE, 0xAD, 0xBE, 0xEF),
			) + `"`,
			errContains: ErrExtraneousData,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseChallenge(tt.header)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tt.header)
			}

			if !errors.Is(err, tt.errContains) {
				t.Errorf("expected error matching %v, got %v", tt.errContains, err)
			}
		})
	}

	t.Run("Base64WithPaddingTolerated", func(t *testing.T) {
		t.Parallel()

		header := `PrivateToken challenge="` + rsaChallengeB64 + `=="`

		tc, err := ParseChallenge(header)
		if err != nil {
			t.Fatalf("expected success with padded base64url, got: %v", err)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("issuer mismatch")
		}
	})

	t.Run("GiantHeaderDoSProtection", func(t *testing.T) {
		t.Parallel()

		giant := "PrivateToken dummy=" + strings.Repeat("A", 100000) + `, challenge="` + rsaChallengeB64 + `"`

		tc, err := ParseChallenge(giant)
		if err != nil {
			t.Fatalf("giant header failed: %v", err)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("issuer mismatch")
		}
	})
}

func TestFastPathDetection(t *testing.T) {
	t.Parallel()

	t.Run("IsPrivateTokenChallenge", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			header string
			expect bool
		}{
			{`PrivateToken challenge="ch"`, true},
			{`privatetoken challenge="ch"`, true},
			{`PRIVATETOKEN challenge="ch"`, true},
			{`Basic realm="api", PrivateToken challenge="ch"`, true},
			{`PrivateToken`, true},
			{`NotPrivateToken challenge="ch"`, false},
			{`PrivateTokenXYZ challenge="ch"`, false},
			{`Basic realm="api"`, false},
			{`Bearer token`, false},
			{``, false},
		}

		for _, c := range cases {
			res := IsPrivateTokenChallenge(c.header)
			if res != c.expect {
				t.Errorf("IsPrivateTokenChallenge(%q) = %v, want %v", c.header, res, c.expect)
			}
		}
	})

	t.Run("HasPrivateTokenChallenge", func(t *testing.T) {
		t.Parallel()

		h := http.Header{}
		h.Add("WWW-Authenticate", `Basic realm="foo"`)

		if HasPrivateTokenChallenge(h) {
			t.Error("expected false for Basic only")
		}

		h.Add("WWW-Authenticate", `PrivateToken challenge="ch"`)

		if !HasPrivateTokenChallenge(h) {
			t.Error("expected true when PrivateToken present")
		}

		if HasPrivateTokenChallenge(nil) {
			t.Error("expected false for nil header")
		}
	})
}

func TestAuthorization_FormatAndParse(t *testing.T) {
	t.Parallel()

	var (
		nonce           [32]byte
		challengeDigest [32]byte
		tokenKeyID      [32]byte
	)
	for i := range nonce {
		nonce[i] = byte(i + 1)
		challengeDigest[i] = byte(i + 33)
		tokenKeyID[i] = byte(i + 65)
	}

	auth := make([]byte, 256)
	for i := range auth {
		auth[i] = 0x77
	}

	tok := &Token{
		TokenType:       TokenTypeBlindRSA,
		Nonce:           nonce,
		ChallengeDigest: challengeDigest,
		TokenKeyID:      tokenKeyID,
		Authenticator:   auth,
	}

	t.Run("FormatAndParseRoundTrip", func(t *testing.T) {
		t.Parallel()

		headerVal := FormatAuthorization(tok)
		if !strings.HasPrefix(headerVal, `PrivateToken token="`) || !strings.HasSuffix(headerVal, `"`) {
			t.Fatalf("unexpected header format: %s", headerVal)
		}

		parsed, err := ParseAuthorization(headerVal)
		if err != nil {
			t.Fatalf("ParseAuthorization failed: %v", err)
		}

		if parsed.TokenType != tok.TokenType {
			t.Errorf("TokenType mismatch")
		}

		if parsed.Nonce != tok.Nonce {
			t.Errorf("Nonce mismatch")
		}

		if parsed.ChallengeDigest != tok.ChallengeDigest {
			t.Errorf("ChallengeDigest mismatch")
		}

		if parsed.TokenKeyID != tok.TokenKeyID {
			t.Errorf("TokenKeyID mismatch")
		}

		if !bytes.Equal(parsed.Authenticator, tok.Authenticator) {
			t.Errorf("Authenticator mismatch")
		}
	})

	t.Run("FormatAuthorizationBytes_MatchesFormatAuthorization", func(t *testing.T) {
		t.Parallel()

		raw, err := MarshalToken(tok)
		if err != nil {
			t.Fatalf("MarshalToken failed: %v", err)
		}

		h1 := FormatAuthorization(tok)

		h2 := FormatAuthorizationBytes(raw)
		if h1 != h2 {
			t.Errorf("mismatch:\nh1: %s\nh2: %s", h1, h2)
		}
	})

	t.Run("FormatAuthorizationStrict", func(t *testing.T) {
		t.Parallel()

		_, err := FormatAuthorizationStrict(nil)
		if !errors.Is(err, ErrNilToken) {
			t.Errorf("expected ErrNilToken, got %v", err)
		}

		emptyAuth := &Token{TokenType: TokenTypeBlindRSA, Authenticator: nil}

		_, err = FormatAuthorizationStrict(emptyAuth)
		if !errors.Is(err, ErrEmptyAuthenticator) {
			t.Errorf("expected ErrEmptyAuthenticator, got %v", err)
		}
	})

	t.Run("ParseAuthorization_UnquotedAndToken68", func(t *testing.T) {
		t.Parallel()

		raw, _ := MarshalToken(tok)
		b64 := EncodeBase64URL(raw)

		// Unquoted token=
		h1 := `PrivateToken token=` + b64

		p1, err := ParseAuthorization(h1)
		if err != nil {
			t.Fatalf("ParseAuthorization unquoted failed: %v", err)
		}

		if p1.TokenType != tok.TokenType {
			t.Errorf("TokenType mismatch")
		}

		// Raw token68
		h2 := `PrivateToken ` + b64

		p2, err := ParseAuthorization(h2)
		if err != nil {
			t.Fatalf("ParseAuthorization token68 failed: %v", err)
		}

		if p2.TokenType != tok.TokenType {
			t.Errorf("TokenType mismatch")
		}
	})

	t.Run("ParseAuthorization_Boundaries", func(t *testing.T) {
		t.Parallel()
		// Empty
		_, err := ParseAuthorization("")
		if !errors.Is(err, ErrMalformedAuthorization) {
			t.Errorf("expected ErrMalformedAuthorization for empty, got %v", err)
		}

		// Foreign scheme
		_, err = ParseAuthorization(`Bearer abc`)
		if !errors.Is(err, ErrMalformedAuthorization) {
			t.Errorf("expected ErrMalformedAuthorization for Bearer, got %v", err)
		}

		// Missing token parameter
		_, err = ParseAuthorization(`PrivateToken`)
		if !errors.Is(err, ErrMissingTokenParam) {
			t.Errorf("expected ErrMissingTokenParam, got %v", err)
		}

		// Corrupt base64
		_, err = ParseAuthorization(`PrivateToken token="!@#$"`)
		if !errors.Is(err, ErrInvalidEncoding) {
			t.Errorf("expected ErrInvalidEncoding, got %v", err)
		}

		// Truncated binary token in base64
		_, err = ParseAuthorization(`PrivateToken token="AAAA"`)
		if !errors.Is(err, ErrTruncatedToken) && !errors.Is(err, ErrTruncatedWire) {
			t.Errorf("expected ErrTruncatedToken, got %v", err)
		}
	})
}

func TestBase64URL_Encoding(t *testing.T) {
	t.Parallel()

	data := []byte("Testing RFC 4648 section 5 unpadded base64url encoding!")

	encoded := EncodeBase64URL(data)
	if strings.Contains(encoded, "=") || strings.Contains(encoded, "+") || strings.Contains(encoded, "/") {
		t.Errorf("encoded string contains non-URL or padding characters: %s", encoded)
	}

	decoded, err := DecodeBase64URL(encoded)
	if err != nil {
		t.Fatalf("DecodeBase64URL failed: %v", err)
	}

	if !bytes.Equal(decoded, data) {
		t.Errorf("decoded data mismatch")
	}

	// Test fallback: padded URL-safe
	paddedURL := encoded + "=="

	d2, err := DecodeBase64URL(paddedURL)
	if err != nil || !bytes.Equal(d2, data) {
		t.Errorf("failed padded URL-safe decode: err=%v", err)
	}

	// Test fallback: standard base64
	stdEncoded := "VGVzdGluZyBzdGFuZGFyZCBiYXNlNjQ="

	d3, err := DecodeBase64URL(stdEncoded)
	if err != nil || string(d3) != "Testing standard base64" {
		t.Errorf("failed standard base64 fallback: err=%v", err)
	}

	// Empty string
	_, err = DecodeBase64URL("")
	if !errors.Is(err, ErrInvalidEncoding) {
		t.Errorf("expected ErrInvalidEncoding for empty string, got %v", err)
	}
}

func TestChallenge_Concurrent(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 100
		iterations = 500
	)

	header := `PrivateToken challenge="` + rsaChallengeB64 + `", token-key="dGVzdGtleQ", max-age=3600`

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()

			tok := &Token{
				TokenType:     TokenTypeBlindRSA,
				Authenticator: []byte{byte(gID), 0x02, 0x03},
			}

			for i := 0; i < iterations; i++ {
				// 1. ParseChallenge
				tc, err := ParseChallenge(header)
				if err != nil || tc.IssuerName != "issuer.example.com" {
					t.Errorf("concurrent ParseChallenge failed: %v", err)
				}

				// 2. IsPrivateTokenChallenge
				if !IsPrivateTokenChallenge(header) {
					t.Errorf("concurrent IsPrivateTokenChallenge failed")
				}

				// 3. FormatAuthorization & ParseAuthorization
				authHdr := FormatAuthorization(tok)

				parsedTok, err := ParseAuthorization(authHdr)
				if err != nil || parsedTok.Authenticator[0] != byte(gID) {
					t.Errorf("concurrent ParseAuthorization failed: %v", err)
				}
			}
		}(g)
	}

	wg.Wait()
}

func BenchmarkParseChallenge(b *testing.B) {
	header := `PrivateToken challenge="` + rsaChallengeB64 + `", token-key="dGVzdGtleQ", max-age=3600`

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		c, err := ParseChallenge(header)
		if err != nil || c == nil {
			b.Fatal("unexpected parse failure")
		}
	}
}

func BenchmarkParseChallenge_Parallel(b *testing.B) {
	header := `PrivateToken challenge="` + rsaChallengeB64 + `"`

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c, err := ParseChallenge(header)
			if err != nil || c == nil {
				b.Fatal("unexpected parse failure")
			}
		}
	})
}

func BenchmarkIsPrivateTokenChallenge(b *testing.B) {
	header := `Basic realm="restricted", PrivateToken challenge="` + rsaChallengeB64 + `"`

	// Programmatic assertion: 0 heap allocations
	allocs := testing.AllocsPerRun(100, func() {
		_ = IsPrivateTokenChallenge(header)
	})
	if allocs > 0 {
		b.Fatalf("expected 0 allocations, got %v", allocs)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = IsPrivateTokenChallenge(header)
	}
}

func BenchmarkFormatAuthorization(b *testing.B) {
	tok := &Token{
		TokenType:     TokenTypeBlindRSA,
		Authenticator: make([]byte, 256),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		s := FormatAuthorization(tok)
		if len(s) == 0 {
			b.Fatal("unexpected format failure")
		}
	}
}

func BenchmarkDecodeBase64URL(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		bBytes, err := DecodeBase64URL(rsaChallengeB64)
		if err != nil || len(bBytes) == 0 {
			b.Fatal("unexpected decode failure")
		}
	}
}
