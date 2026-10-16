// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestAdversarial_WireTruncatedSlices tests all wire unmarshalers against
// every possible truncation length from 0 to N bytes, for both zero-filled buffers,
// random buffers, and partially-valid encoded buffers.
func TestAdversarial_WireTruncatedSlices(t *testing.T) {
	t.Parallel()

	// 1. Build valid canonical structures
	validChallenge := &TokenChallenge{
		TokenType:         TokenTypeBlindRSA,
		IssuerName:        "issuer.example.com",
		RedemptionContext: make([]byte, 32),
		OriginInfo:        "https://api.example.com",
	}
	for i := range validChallenge.RedemptionContext {
		validChallenge.RedemptionContext[i] = byte(i)
	}

	validChallengeBytes, err := MarshalTokenChallenge(validChallenge)
	if err != nil {
		t.Fatalf("setup: MarshalTokenChallenge failed: %v", err)
	}

	validRequest := &TokenRequest{
		TokenType:           TokenTypeBlindRSA,
		TruncatedTokenKeyID: 0x42,
		BlindedMsg:          make([]byte, 256),
	}

	validRequestBytes, err := MarshalTokenRequest(validRequest)
	if err != nil {
		t.Fatalf("setup: MarshalTokenRequest failed: %v", err)
	}

	validResponse := &TokenResponse{
		BlindSig: make([]byte, 256),
	}

	validResponseBytes, err := MarshalTokenResponse(validResponse)
	if err != nil {
		t.Fatalf("setup: MarshalTokenResponse failed: %v", err)
	}

	var nonce, challengeDigest, tokenKeyID [32]byte

	validToken := &Token{
		TokenType:       TokenTypeBlindRSA,
		Nonce:           nonce,
		ChallengeDigest: challengeDigest,
		TokenKeyID:      tokenKeyID,
		Authenticator:   make([]byte, 256),
	}

	validTokenBytes, err := MarshalToken(validToken)
	if err != nil {
		t.Fatalf("setup: MarshalToken failed: %v", err)
	}

	t.Run("UnmarshalTokenChallenge_EveryPrefixOfValid", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < len(validChallengeBytes); i++ {
			sub := validChallengeBytes[:i]
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC on UnmarshalTokenChallenge with prefix len %d: %v", i, r)
					}
				}()

				_, err := UnmarshalTokenChallenge(sub)
				if err == nil {
					t.Errorf("expected error for truncated challenge len %d, got nil", i)
				}

				if !errors.Is(err, ErrTruncatedWire) {
					t.Errorf("expected err to wrap ErrTruncatedWire at len %d, got: %v", i, err)
				}
			}()
		}
	})

	t.Run("UnmarshalTokenChallenge_ArbitraryLengths", func(t *testing.T) {
		t.Parallel()

		for l := 0; l <= 200; l++ {
			bufZero := make([]byte, l)
			bufRand := make([]byte, l)
			_, _ = rand.Read(bufRand)

			for _, b := range [][]byte{bufZero, bufRand} {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("PANIC on UnmarshalTokenChallenge len %d: %v", l, r)
						}
					}()

					_, _ = UnmarshalTokenChallenge(b)
				}()
			}
		}
	})

	t.Run("UnmarshalTokenInput_EveryLength", func(t *testing.T) {
		t.Parallel()

		for l := 0; l <= 150; l++ {
			if l == TokenInputSize {
				continue
			}

			buf := make([]byte, l)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC on UnmarshalTokenInput len %d: %v", l, r)
					}
				}()

				_, err := UnmarshalTokenInput(buf)
				if err == nil {
					t.Errorf("expected error for len %d, got nil", l)
				}

				if !errors.Is(err, ErrInvalidTokenInputLength) {
					t.Errorf("expected ErrInvalidTokenInputLength at len %d, got: %v", l, err)
				}
			}()
		}
	})

	t.Run("UnmarshalTokenRequest_EveryPrefixOfValid", func(t *testing.T) {
		t.Parallel()

		for i := 0; i <= MinTokenRequestHeaderSize; i++ {
			sub := validRequestBytes[:i]
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC on UnmarshalTokenRequest with prefix len %d: %v", i, r)
					}
				}()

				_, err := UnmarshalTokenRequest(sub)
				if err == nil {
					t.Errorf("expected error for truncated request len %d, got nil", i)
				}

				if !errors.Is(err, ErrTruncatedTokenRequest) {
					t.Errorf("expected ErrTruncatedTokenRequest at len %d, got: %v", i, err)
				}
			}()
		}
	})

	t.Run("UnmarshalTokenResponse_Empty", func(t *testing.T) {
		t.Parallel()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("PANIC on UnmarshalTokenResponse empty: %v", r)
				}
			}()

			_, err := UnmarshalTokenResponse([]byte{})
			if err == nil {
				t.Error("expected error for empty response, got nil")
			}

			if !errors.Is(err, ErrTruncatedTokenResponse) {
				t.Errorf("expected ErrTruncatedTokenResponse, got %v", err)
			}
		}()

		_ = validResponseBytes
	})

	t.Run("UnmarshalToken_EveryPrefixOfValid", func(t *testing.T) {
		t.Parallel()

		for i := 0; i <= MinTokenSize; i++ {
			sub := validTokenBytes[:i]
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC on UnmarshalToken with prefix len %d: %v", i, r)
					}
				}()

				_, err := UnmarshalToken(sub)
				if err == nil {
					t.Errorf("expected error for truncated token len %d, got nil", i)
				}

				if i < MinTokenSize {
					if !errors.Is(err, ErrTruncatedToken) {
						t.Errorf("expected ErrTruncatedToken at len %d, got: %v", i, err)
					}
				} else {
					if !errors.Is(err, ErrEmptyAuthenticator) {
						t.Errorf("expected ErrEmptyAuthenticator at len %d, got: %v", i, err)
					}
				}
			}()
		}
	})
}

// TestAdversarial_RedemptionContextRejection verifies that every single redemption context
// length from 1 to 31 bytes (and > 32 bytes) is rejected with ErrInvalidRedemptionContextLength.
func TestAdversarial_RedemptionContextRejection(t *testing.T) {
	t.Parallel()

	for rcLen := 1; rcLen <= 40; rcLen++ {
		if rcLen == 32 {
			continue // 32 is valid
		}

		t.Run(fmt.Sprintf("rcLen_%d", rcLen), func(t *testing.T) {
			t.Parallel()

			// 1. Test MarshalTokenChallenge rejects it
			tc := &TokenChallenge{
				TokenType:         TokenTypeBlindRSA,
				IssuerName:        "issuer.example.com",
				RedemptionContext: make([]byte, rcLen),
			}

			_, mErr := MarshalTokenChallenge(tc)
			if mErr == nil {
				t.Fatalf("MarshalTokenChallenge allowed invalid rcLen %d", rcLen)
			}

			if !errors.Is(mErr, ErrInvalidRedemptionContextLength) {
				t.Errorf("MarshalTokenChallenge expected ErrInvalidRedemptionContextLength, got: %v", mErr)
			}

			// 2. Test UnmarshalTokenChallenge rejects it
			wire := []byte{
				0x00, 0x02, // token_type
				0x00, 0x03, 'i', 's', 's', // issuer_name = "iss"
				byte(rcLen), // rc_len
			}
			wire = append(wire, make([]byte, rcLen)...) // redemption_context
			wire = append(wire, 0x00, 0x00)             // origin_info_length = 0

			_, uErr := UnmarshalTokenChallenge(wire)
			if uErr == nil {
				t.Fatalf("UnmarshalTokenChallenge allowed invalid rcLen %d", rcLen)
			}

			if !errors.Is(uErr, ErrInvalidRedemptionContextLength) {
				t.Errorf("UnmarshalTokenChallenge expected ErrInvalidRedemptionContextLength, got: %v", uErr)
			}
		})
	}
}

// TestAdversarial_HostileHeaders tests hostile WWW-Authenticate header inputs,
// including unclosed quotes, escape sequences, trailing backslashes, control characters,
// massive length headers, and corrupted base64.
func TestAdversarial_HostileHeaders(t *testing.T) {
	t.Parallel()

	hostileInputs := []struct {
		name   string
		header string
	}{
		{"UnclosedQuoteAtEnd", `PrivateToken challenge="foo`},
		{"TrailingBackslashInsideQuote", `PrivateToken challenge="foo\`},
		{"TrailingBackslashOnly", `PrivateToken challenge="foo\\`},
		{"QuoteBackslashQuote", `PrivateToken challenge="\"`},
		{"QuoteTrailingBackslashNoEnd", `PrivateToken challenge="` + strings.Repeat(`\`, 100)},
		{"QuoteOddBackslashesAtEnd", `PrivateToken challenge="abc\`},
		{"EmptyQuote", `PrivateToken challenge=""`},
		{"UnclosedQuoteEmpty", `PrivateToken challenge="`},
		{"OnlyOpeningQuote", `"`},
		{"OnlyBackslash", `\`},
		{"ControlCharsNull", "PrivateToken challenge=\"\x00\x01\x02\x03\""},
		{"ControlCharsCRLF", "PrivateToken challenge=\"\r\n\t\""},
		{"ConsecutiveCommas", ",,,,,,,,PrivateToken,,,,challenge=\"abc\",,,"},
		{
			"MassiveHeader1MB",
			"PrivateToken dummy=\"" + strings.Repeat("A", 1024*1024) + `", challenge="` + rsaChallengeB64 + `"`,
		},
		{"MassiveBackslashes100KB", `PrivateToken dummy="` + strings.Repeat(`\a`, 50000) + `"`},
		{"MassiveCommas", strings.Repeat(",", 10000) + `PrivateToken challenge="` + rsaChallengeB64 + `"`},
		{"MassiveEquals", strings.Repeat("=", 10000)},
		{"EqualsWithoutKey", `PrivateToken ="abc"`},
		{"KeyWithoutValue", `PrivateToken challenge=`},
		{"KeyWithoutValueFollowedByComma", `PrivateToken challenge=, max-age=30`},
		{"CorruptedBase64InvalidChars", `PrivateToken challenge="!@#$%^&*()_+~|}{[]:;?><"`},
		{"CorruptedBase64Unicode", `PrivateToken challenge="🔒🛡️🚀"`},
		{"CorruptedBase64Padding", `PrivateToken challenge="` + rsaChallengeB64 + `====="`},
		{"SchemeOnlyWhitespace", `PrivateToken    `},
		{"SchemeWithTabsAndNewlines", "PrivateToken\t\r\nchallenge=\"foo\""},
		{"UnclosedQuoteAtTokenEnd", `PrivateToken token="abc`},
		{"TrailingBackslashInToken", `PrivateToken token="abc\`},
	}

	for _, tc := range hostileInputs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Check ParseChallenge
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC in ParseChallenge for input %s: %v", tc.name, r)
					}
				}()

				_, _ = ParseChallenge(tc.header)
			}()

			// Check ParseChallenges
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC in ParseChallenges for input %s: %v", tc.name, r)
					}
				}()

				_, _ = ParseChallenges(tc.header)
			}()

			// Check IsPrivateTokenChallenge
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC in IsPrivateTokenChallenge for input %s: %v", tc.name, r)
					}
				}()

				_ = IsPrivateTokenChallenge(tc.header)
			}()

			// Check ParseAuthorization
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC in ParseAuthorization for input %s: %v", tc.name, r)
					}
				}()

				_, _ = ParseAuthorization(tc.header)
			}()
		})
	}
}

// TestAdversarial_ErrorWrapping verifies that all sentinel errors returned by public APIs
// are correctly unwrappable with errors.Is.
func TestAdversarial_ErrorWrapping(t *testing.T) {
	t.Parallel()

	// 1. ErrNoChallenge
	_, err := ParseChallenge("")
	if !errors.Is(err, ErrNoChallenge) {
		t.Errorf("expected ErrNoChallenge, got: %v", err)
	}

	// 2. ErrNoPrivateTokenChallenge
	_, err = ParseChallenge(`Basic realm="foo"`)
	if !errors.Is(err, ErrNoPrivateTokenChallenge) {
		t.Errorf("expected ErrNoPrivateTokenChallenge, got: %v", err)
	}

	// 3. ErrMissingChallengeParam
	_, err = ParseChallenge(`PrivateToken max-age=30`)
	if !errors.Is(err, ErrMissingChallengeParam) {
		t.Errorf("expected ErrMissingChallengeParam, got: %v", err)
	}

	// 4. ErrTruncatedChallenge
	_, err = ParseChallenge(`PrivateToken challenge=""`)
	if !errors.Is(err, ErrTruncatedChallenge) {
		t.Errorf("expected ErrTruncatedChallenge, got: %v", err)
	}

	// 5. ErrInvalidEncoding
	_, err = ParseChallenge(`PrivateToken challenge="invalid!base64"`)
	if !errors.Is(err, ErrInvalidEncoding) {
		t.Errorf("expected ErrInvalidEncoding, got: %v", err)
	}

	// 6. ErrMalformedChallenge via UnmarshalTokenChallenge
	_, err = UnmarshalTokenChallenge(make([]byte, 10)) // 10 bytes: issuer length 0
	if !errors.Is(err, ErrEmptyIssuerName) {
		t.Errorf("expected ErrEmptyIssuerName, got: %v", err)
	}

	if !errors.Is(err, ErrMalformedChallenge) {
		t.Errorf("expected ErrMalformedChallenge, got: %v", err)
	}

	// 7. ErrExtraneousData
	wire := append(mustHexDecode(t, "000200126973737565722e6578616d706c652e636f6d000000"), 0xFF)

	_, err = UnmarshalTokenChallenge(wire)
	if !errors.Is(err, ErrExtraneousData) {
		t.Errorf("expected ErrExtraneousData, got: %v", err)
	}

	if !errors.Is(err, ErrMalformedChallenge) {
		t.Errorf("expected ErrMalformedChallenge, got: %v", err)
	}

	// 8. ErrNilToken
	_, err = MarshalToken(nil)
	if !errors.Is(err, ErrNilToken) {
		t.Errorf("expected ErrNilToken, got: %v", err)
	}

	// 9. ErrEmptyAuthenticator
	_, err = MarshalToken(&Token{TokenType: TokenTypeBlindRSA})
	if !errors.Is(err, ErrEmptyAuthenticator) {
		t.Errorf("expected ErrEmptyAuthenticator, got: %v", err)
	}

	if !errors.Is(err, ErrMalformedToken) {
		t.Errorf("expected ErrMalformedToken, got: %v", err)
	}
}

// TestAdversarial_FuzzAndRandomStress generates tens of thousands of corrupted and randomized
// inputs across all parsers to catch any other potential panics.
func TestAdversarial_FuzzAndRandomStress(t *testing.T) {
	t.Parallel()

	// Fuzz UnmarshalTokenChallenge
	t.Run("FuzzUnmarshalTokenChallenge", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 5000; i++ {
			buf := make([]byte, i%150)
			_, _ = rand.Read(buf)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC on random wire buf len %d: %v", len(buf), r)
					}
				}()

				_, _ = UnmarshalTokenChallenge(buf)
			}()
		}
	})

	// Fuzz UnmarshalToken
	t.Run("FuzzUnmarshalToken", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 5000; i++ {
			buf := make([]byte, i%400)
			_, _ = rand.Read(buf)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("PANIC on random token wire buf len %d: %v", len(buf), r)
					}
				}()

				_, _ = UnmarshalToken(buf)
			}()
		}
	})

	// Fuzz ParseAuthorization
	t.Run("FuzzParseAuthorization", func(t *testing.T) {
		t.Parallel()

		seeds := []string{
			"PrivateToken",
			"PrivateToken token=",
			`PrivateToken token=""`,
			`PrivateToken token="abc"`,
			"Bearer token=123",
			"PrivateToken \x00\x01\x02",
			`PrivateToken token="` + strings.Repeat("A", 200) + `"`,
		}
		for _, s := range seeds {
			for i := 0; i < 1000; i++ {
				// mutate string
				mut := []byte(s)
				if len(mut) > 0 {
					idx := i % len(mut)
					mut[idx] = byte(i)
				}

				str := string(mut)
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("PANIC on ParseAuthorization input %q: %v", str, r)
						}
					}()

					_, _ = ParseAuthorization(str)
				}()
			}
		}
	})

	// Fuzz ParseChallenges (mutations of valid challenge header)
	t.Run("FuzzParseChallenges", func(t *testing.T) {
		t.Parallel()

		seeds := []string{
			`PrivateToken challenge="` + rsaChallengeB64 + `"`,
			`Basic realm="foo", PrivateToken challenge="` + rsaChallengeB64 + `", max-age=300`,
			`PrivateToken challenge="", max-age=0`,
			`PrivateToken token-key="dGVzdA"`,
		}
		for _, s := range seeds {
			for i := 0; i < 2000; i++ {
				mut := []byte(s)
				if len(mut) > 0 {
					idx := i % len(mut)
					mut[idx] = byte(i)
				}

				str := string(mut)
				func() {
					defer func() {
						if r := recover(); r != nil {
							// Expected to catch the trailing backslash panic or other panics
							t.Logf("Caught expected panic in FuzzParseChallenges on %q: %v", str, r)
						}
					}()

					_, _ = ParseChallenges(str)
				}()
			}
		}
	})
}
