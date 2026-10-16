// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
)

func mustHexDecode(t *testing.T, s string) []byte {
	t.Helper()

	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("mustHexDecode: invalid hex string: %v", err)
	}

	return b
}

func TestTokenChallenge_RFC9578Vectors(t *testing.T) {
	t.Parallel()

	t.Run("Vector1_BlindRSA_Minimal", func(t *testing.T) {
		t.Parallel()
		// Vector 1: Standard Blind RSA TokenChallenge (Minimal, Context-Free)
		// TokenType: 0x0002
		// IssuerName: "issuer.example.com" (18 bytes)
		// RedemptionContext: empty (0 bytes)
		// OriginInfo: empty (0 bytes)
		rawHex := "000200126973737565722e6578616d706c652e636f6d000000"
		wireBytes := mustHexDecode(t, rawHex)

		tc, err := UnmarshalTokenChallenge(wireBytes)
		if err != nil {
			t.Fatalf("UnmarshalTokenChallenge failed: %v", err)
		}

		if tc.TokenType != TokenTypeBlindRSA {
			t.Errorf("expected TokenType 0x%04x, got 0x%04x", TokenTypeBlindRSA, tc.TokenType)
		}

		if tc.IssuerName != "issuer.example.com" {
			t.Errorf("expected IssuerName issuer.example.com, got %s", tc.IssuerName)
		}

		if len(tc.RedemptionContext) != 0 {
			t.Errorf("expected empty RedemptionContext, got len %d", len(tc.RedemptionContext))
		}

		if tc.OriginInfo != "" {
			t.Errorf("expected empty OriginInfo, got %s", tc.OriginInfo)
		}

		expectedDigest := sha256.Sum256(wireBytes)
		if tc.Digest != expectedDigest {
			t.Errorf("expected Digest %x, got %x", expectedDigest, tc.Digest)
		}

		marshaled, err := MarshalTokenChallenge(tc)
		if err != nil {
			t.Fatalf("MarshalTokenChallenge failed: %v", err)
		}

		if !bytes.Equal(marshaled, wireBytes) {
			t.Errorf("marshaled wire bytes mismatch:\nexpected: %x\ngot:      %x", wireBytes, marshaled)
		}
	})

	t.Run("Vector2_BlindRSA_ContextBound", func(t *testing.T) {
		t.Parallel()
		// Vector 2: Context-Bound TokenChallenge with Origin Info
		// TokenType: 0x0002
		// IssuerName: "issuer.example.com" (18 bytes)
		// RedemptionContext: 32 bytes (0x01..0x20)
		// OriginInfo: "https://api.example.com" (23 bytes)
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

		wireBytes, err := MarshalTokenChallenge(original)
		if err != nil {
			t.Fatalf("MarshalTokenChallenge failed: %v", err)
		}

		// Expected length: 2 + 2 + 18 + 1 + 32 + 2 + 23 = 80 bytes
		if len(wireBytes) != 80 {
			t.Fatalf("expected wire length 80, got %d", len(wireBytes))
		}

		tc, err := UnmarshalTokenChallenge(wireBytes)
		if err != nil {
			t.Fatalf("UnmarshalTokenChallenge failed: %v", err)
		}

		if tc.TokenType != TokenTypeBlindRSA {
			t.Errorf("expected TokenType 0x%04x, got 0x%04x", TokenTypeBlindRSA, tc.TokenType)
		}

		if tc.IssuerName != original.IssuerName {
			t.Errorf("expected IssuerName %s, got %s", original.IssuerName, tc.IssuerName)
		}

		if !bytes.Equal(tc.RedemptionContext, original.RedemptionContext) {
			t.Errorf("RedemptionContext mismatch")
		}

		if tc.OriginInfo != original.OriginInfo {
			t.Errorf("expected OriginInfo %s, got %s", original.OriginInfo, tc.OriginInfo)
		}
	})

	t.Run("Vector3_VOPRF", func(t *testing.T) {
		t.Parallel()
		// Vector 3: VOPRF TokenChallenge
		// TokenType: 0x0001
		// IssuerName: "voprf.cloudflare.com" (20 bytes)
		// RedemptionContext: empty
		// OriginInfo: empty
		rawHex := "00010014766f7072662e636c6f7564666c6172652e636f6d000000"
		wireBytes := mustHexDecode(t, rawHex)

		tc, err := UnmarshalTokenChallenge(wireBytes)
		if err != nil {
			t.Fatalf("UnmarshalTokenChallenge failed: %v", err)
		}

		if tc.TokenType != TokenTypeVOPRF {
			t.Errorf("expected TokenType 0x%04x, got 0x%04x", TokenTypeVOPRF, tc.TokenType)
		}

		if tc.IssuerName != "voprf.cloudflare.com" {
			t.Errorf("expected IssuerName voprf.cloudflare.com, got %s", tc.IssuerName)
		}

		remarshaled, err := MarshalTokenChallenge(tc)
		if err != nil {
			t.Fatalf("MarshalTokenChallenge failed: %v", err)
		}

		if !bytes.Equal(remarshaled, wireBytes) {
			t.Errorf("remarshaled mismatch")
		}
	})
}

func TestTokenChallenge_UnmarshalBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wire        []byte
		errContains error
	}{
		{
			name:        "TC-Trunc-0",
			wire:        []byte{},
			errContains: ErrTruncatedWire,
		},
		{
			name:        "TC-Trunc-1",
			wire:        []byte{0x00},
			errContains: ErrTruncatedWire,
		},
		{
			name:        "TC-Trunc-4",
			wire:        []byte{0x00, 0x02, 0x00, 0x05},
			errContains: ErrTruncatedWire,
		},
		{
			name:        "TC-Trunc-7",
			wire:        []byte{0x00, 0x02, 0x00, 0x01, 'a', 0x00, 0x00},
			errContains: ErrTruncatedWire,
		},
		{
			name:        "TC-Issuer-Zero",
			wire:        []byte{0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			errContains: ErrEmptyIssuerName,
		},
		{
			name:        "TC-Issuer-Overflow",
			wire:        []byte{0x00, 0x02, 0x00, 0x20, 'a', 'b', 'c', 0x00, 0x00, 0x00},
			errContains: ErrTruncatedWire,
		},
		{
			name: "TC-Context-Invalid-1",
			wire: []byte{
				0x00, 0x02, 0x00, 0x03, 'i', 's', 's',
				0x01, 0xAA, // rc_len = 1
				0x00, 0x00,
			},
			errContains: ErrInvalidRedemptionContextLength,
		},
		{
			name: "TC-Context-Invalid-16",
			wire: append(
				[]byte{0x00, 0x02, 0x00, 0x03, 'i', 's', 's', 0x10},
				append(make([]byte, 16), 0x00, 0x00)...,
			),
			errContains: ErrInvalidRedemptionContextLength,
		},
		{
			name: "TC-Context-Invalid-33",
			wire: append(
				[]byte{0x00, 0x02, 0x00, 0x03, 'i', 's', 's', 0x21},
				append(make([]byte, 33), 0x00, 0x00)...,
			),
			errContains: ErrInvalidRedemptionContextLength,
		},
		{
			name: "TC-Context-Trunc-32",
			wire: []byte{
				0x00, 0x02, 0x00, 0x03, 'i', 's', 's',
				0x20,             // claims 32 bytes
				0x01, 0x02, 0x03, // only 3 bytes present
			},
			errContains: ErrTruncatedWire,
		},
		{
			name: "TC-Origin-Trunc",
			wire: []byte{
				0x00, 0x02, 0x00, 0x03, 'i', 's', 's',
				0x00,       // rc_len = 0
				0x00, 0x10, // claims 16 bytes origin
				'a', 'b', 'c', // only 3 bytes
			},
			errContains: ErrTruncatedWire,
		},
		{
			name: "TC-Trailing-Garbage",
			wire: append(
				mustHexDecode(t, "000200126973737565722e6578616d706c652e636f6d000000"),
				0xDE, 0xAD, 0xBE, 0xEF,
			),
			errContains: ErrExtraneousData,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tc, err := UnmarshalTokenChallenge(tt.wire)
			if err == nil {
				t.Fatalf("expected error, got nil (result: %+v)", tc)
			}

			if !errors.Is(err, tt.errContains) {
				t.Errorf("expected error matching %v, got %v", tt.errContains, err)
			}
		})
	}
}

func TestTokenChallenge_MarshalBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("NilPointer", func(t *testing.T) {
		t.Parallel()

		_, err := MarshalTokenChallenge(nil)
		if err == nil {
			t.Fatal("expected error for nil challenge")
		}

		if !errors.Is(err, ErrTruncatedChallenge) && !errors.Is(err, ErrTruncatedWire) {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("EmptyIssuer", func(t *testing.T) {
		t.Parallel()

		tc := &TokenChallenge{
			TokenType:  TokenTypeBlindRSA,
			IssuerName: "",
		}

		_, err := MarshalTokenChallenge(tc)
		if err == nil {
			t.Fatal("expected error for empty issuer")
		}

		if !errors.Is(err, ErrEmptyIssuerName) {
			t.Errorf("expected ErrEmptyIssuerName, got %v", err)
		}
	})

	t.Run("InvalidContextLength", func(t *testing.T) {
		t.Parallel()

		tc := &TokenChallenge{
			TokenType:         TokenTypeBlindRSA,
			IssuerName:        "example.com",
			RedemptionContext: []byte{0x01, 0x02}, // len 2
		}

		_, err := MarshalTokenChallenge(tc)
		if err == nil {
			t.Fatal("expected error for invalid redemption context length")
		}

		if !errors.Is(err, ErrInvalidRedemptionContextLength) {
			t.Errorf("expected ErrInvalidRedemptionContextLength, got %v", err)
		}
	})

	t.Run("BinaryMarshalerInterface", func(t *testing.T) {
		t.Parallel()

		tc := &TokenChallenge{
			TokenType:  TokenTypeBlindRSA,
			IssuerName: "example.com",
		}

		data, err := tc.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary failed: %v", err)
		}

		var decoded TokenChallenge
		if err := decoded.UnmarshalBinary(data); err != nil {
			t.Fatalf("UnmarshalBinary failed: %v", err)
		}

		if decoded.IssuerName != tc.IssuerName {
			t.Errorf("expected issuer %s, got %s", tc.IssuerName, decoded.IssuerName)
		}
	})
}

func TestTokenInput_CorrectnessAndBoundaries(t *testing.T) {
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

	t.Run("BuildTokenInput", func(t *testing.T) {
		t.Parallel()

		out := BuildTokenInput(TokenTypeBlindRSA, nonce, challengeDigest, tokenKeyID)
		if len(out) != TokenInputSize {
			t.Fatalf("expected length %d, got %d", TokenInputSize, len(out))
		}

		// Verify big-endian tokenType
		if out[0] != 0x00 || out[1] != 0x02 {
			t.Errorf("expected tokenType 0x0002, got 0x%02x%02x", out[0], out[1])
		}

		// Verify fields
		if !bytes.Equal(out[2:34], nonce[:]) {
			t.Errorf("nonce mismatch")
		}

		if !bytes.Equal(out[34:66], challengeDigest[:]) {
			t.Errorf("challengeDigest mismatch")
		}

		if !bytes.Equal(out[66:98], tokenKeyID[:]) {
			t.Errorf("tokenKeyID mismatch")
		}
	})

	t.Run("MarshalUnmarshalRoundTrip", func(t *testing.T) {
		t.Parallel()

		ti := &TokenInput{
			TokenType:       TokenTypeBlindRSA,
			Nonce:           nonce,
			ChallengeDigest: challengeDigest,
			TokenKeyID:      tokenKeyID,
		}

		arr := MarshalTokenInput(ti)

		parsed, err := UnmarshalTokenInput(arr[:])
		if err != nil {
			t.Fatalf("UnmarshalTokenInput failed: %v", err)
		}

		if parsed.TokenType != ti.TokenType {
			t.Errorf("TokenType mismatch")
		}

		if parsed.Nonce != ti.Nonce {
			t.Errorf("Nonce mismatch")
		}

		if parsed.ChallengeDigest != ti.ChallengeDigest {
			t.Errorf("ChallengeDigest mismatch")
		}

		if parsed.TokenKeyID != ti.TokenKeyID {
			t.Errorf("TokenKeyID mismatch")
		}
	})

	t.Run("NilTokenInputMarshal", func(t *testing.T) {
		t.Parallel()

		arr := MarshalTokenInput(nil)

		var expected [TokenInputSize]byte
		if arr != expected {
			t.Errorf("expected zero array for nil TokenInput")
		}
	})

	t.Run("BoundaryLengths", func(t *testing.T) {
		t.Parallel()
		// 97 bytes
		_, err := UnmarshalTokenInput(make([]byte, 97))
		if !errors.Is(err, ErrInvalidTokenInputLength) {
			t.Errorf("expected ErrInvalidTokenInputLength for 97 bytes, got %v", err)
		}

		// 99 bytes
		_, err = UnmarshalTokenInput(make([]byte, 99))
		if !errors.Is(err, ErrInvalidTokenInputLength) {
			t.Errorf("expected ErrInvalidTokenInputLength for 99 bytes, got %v", err)
		}

		// 0 bytes
		_, err = UnmarshalTokenInput([]byte{})
		if !errors.Is(err, ErrInvalidTokenInputLength) {
			t.Errorf("expected ErrInvalidTokenInputLength for 0 bytes, got %v", err)
		}
	})

	t.Run("TruncateKeyID", func(t *testing.T) {
		t.Parallel()

		var keyID [32]byte

		keyID[31] = 0x42
		if TruncateKeyID(keyID) != 0x42 {
			t.Errorf("expected 0x42, got 0x%02x", TruncateKeyID(keyID))
		}
	})
}

func TestTokenRequest_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	t.Run("ValidRSARequest", func(t *testing.T) {
		t.Parallel()

		blinded := make([]byte, 256)
		for i := range blinded {
			blinded[i] = 0x55
		}

		tr := &TokenRequest{
			TokenType:           TokenTypeBlindRSA,
			TruncatedTokenKeyID: 0x7B,
			BlindedMsg:          blinded,
		}

		wire, err := MarshalTokenRequest(tr)
		if err != nil {
			t.Fatalf("MarshalTokenRequest failed: %v", err)
		}

		if len(wire) != 259 {
			t.Fatalf("expected wire length 259, got %d", len(wire))
		}

		parsed, err := UnmarshalTokenRequest(wire)
		if err != nil {
			t.Fatalf("UnmarshalTokenRequest failed: %v", err)
		}

		if parsed.TokenType != tr.TokenType {
			t.Errorf("TokenType mismatch")
		}

		if parsed.TruncatedTokenKeyID != tr.TruncatedTokenKeyID {
			t.Errorf("TruncatedTokenKeyID mismatch")
		}

		if !bytes.Equal(parsed.BlindedMsg, tr.BlindedMsg) {
			t.Errorf("BlindedMsg mismatch")
		}
	})

	t.Run("NilRequest", func(t *testing.T) {
		t.Parallel()

		_, err := MarshalTokenRequest(nil)
		if !errors.Is(err, ErrTruncatedTokenRequest) && !errors.Is(err, ErrTruncatedWire) {
			t.Errorf("expected ErrTruncatedTokenRequest, got %v", err)
		}
	})

	t.Run("EmptyBlindedMsg", func(t *testing.T) {
		t.Parallel()

		tr := &TokenRequest{
			TokenType:           TokenTypeBlindRSA,
			TruncatedTokenKeyID: 0x01,
			BlindedMsg:          nil,
		}

		_, err := MarshalTokenRequest(tr)
		if !errors.Is(err, ErrEmptyBlindedMsg) {
			t.Errorf("expected ErrEmptyBlindedMsg, got %v", err)
		}
	})

	t.Run("TruncatedBuffer", func(t *testing.T) {
		t.Parallel()

		for l := 0; l <= 3; l++ {
			_, err := UnmarshalTokenRequest(make([]byte, l))
			if !errors.Is(err, ErrTruncatedTokenRequest) && !errors.Is(err, ErrTruncatedWire) {
				t.Errorf("expected ErrTruncatedTokenRequest for len %d, got %v", l, err)
			}
		}
	})
}

func TestTokenResponse_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	t.Run("ValidRSAResponse", func(t *testing.T) {
		t.Parallel()

		sig := make([]byte, 256)
		for i := range sig {
			sig[i] = 0xAA
		}

		tr := &TokenResponse{BlindSig: sig}

		wire, err := MarshalTokenResponse(tr)
		if err != nil {
			t.Fatalf("MarshalTokenResponse failed: %v", err)
		}

		if len(wire) != 256 {
			t.Fatalf("expected wire length 256, got %d", len(wire))
		}

		parsed, err := UnmarshalTokenResponse(wire)
		if err != nil {
			t.Fatalf("UnmarshalTokenResponse failed: %v", err)
		}

		if !bytes.Equal(parsed.BlindSig, tr.BlindSig) {
			t.Errorf("BlindSig mismatch")
		}
	})

	t.Run("NilOrEmptyResponse", func(t *testing.T) {
		t.Parallel()

		_, err := MarshalTokenResponse(nil)
		if !errors.Is(err, ErrTruncatedTokenResponse) && !errors.Is(err, ErrTruncatedWire) {
			t.Errorf("expected ErrTruncatedTokenResponse, got %v", err)
		}

		_, err = MarshalTokenResponse(&TokenResponse{BlindSig: nil})
		if !errors.Is(err, ErrTruncatedTokenResponse) && !errors.Is(err, ErrTruncatedWire) {
			t.Errorf("expected ErrTruncatedTokenResponse, got %v", err)
		}

		_, err = UnmarshalTokenResponse([]byte{})
		if !errors.Is(err, ErrTruncatedTokenResponse) && !errors.Is(err, ErrTruncatedWire) {
			t.Errorf("expected ErrTruncatedTokenResponse, got %v", err)
		}
	})
}

func TestToken_MarshalUnmarshalRoundTrip(t *testing.T) {
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

	t.Run("RSA2048Token_354Bytes", func(t *testing.T) {
		t.Parallel()

		auth := make([]byte, 256)
		for i := range auth {
			auth[i] = byte(i % 256)
		}

		tok := &Token{
			TokenType:       TokenTypeBlindRSA,
			Nonce:           nonce,
			ChallengeDigest: challengeDigest,
			TokenKeyID:      tokenKeyID,
			Authenticator:   auth,
		}

		wire, err := MarshalToken(tok)
		if err != nil {
			t.Fatalf("MarshalToken failed: %v", err)
		}

		if len(wire) != 354 {
			t.Fatalf("expected wire length 354, got %d", len(wire))
		}

		// Structural Identity check: wire[0:98] matches TokenInput
		expectedInput := tok.TokenInput()
		if !bytes.Equal(wire[0:TokenInputSize], expectedInput[:]) {
			t.Errorf("Token wire[0:98] does not match TokenInput")
		}

		parsed, err := UnmarshalToken(wire)
		if err != nil {
			t.Fatalf("UnmarshalToken failed: %v", err)
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

	t.Run("VOPRFToken_130Bytes", func(t *testing.T) {
		t.Parallel()

		auth := make([]byte, 32)
		for i := range auth {
			auth[i] = 0xEE
		}

		tok := &Token{
			TokenType:       TokenTypeVOPRF,
			Nonce:           nonce,
			ChallengeDigest: challengeDigest,
			TokenKeyID:      tokenKeyID,
			Authenticator:   auth,
		}

		wire, err := MarshalToken(tok)
		if err != nil {
			t.Fatalf("MarshalToken failed: %v", err)
		}

		if len(wire) != 130 {
			t.Fatalf("expected wire length 130, got %d", len(wire))
		}

		parsed, err := UnmarshalToken(wire)
		if err != nil {
			t.Fatalf("UnmarshalToken failed: %v", err)
		}

		if parsed.TokenType != TokenTypeVOPRF {
			t.Errorf("TokenType mismatch")
		}
	})

	t.Run("TokenBoundaries", func(t *testing.T) {
		t.Parallel()
		// Nil token
		_, err := MarshalToken(nil)
		if !errors.Is(err, ErrNilToken) {
			t.Errorf("expected ErrNilToken, got %v", err)
		}

		// Empty authenticator marshal
		_, err = MarshalToken(&Token{
			TokenType:     TokenTypeBlindRSA,
			Authenticator: nil,
		})
		if !errors.Is(err, ErrEmptyAuthenticator) {
			t.Errorf("expected ErrEmptyAuthenticator, got %v", err)
		}

		// Truncated token (< 98 bytes)
		_, err = UnmarshalToken(make([]byte, 97))
		if !errors.Is(err, ErrTruncatedToken) && !errors.Is(err, ErrTruncatedWire) {
			t.Errorf("expected ErrTruncatedToken for 97 bytes, got %v", err)
		}

		// Exactly 98 bytes (0 authenticator)
		_, err = UnmarshalToken(make([]byte, 98))
		if !errors.Is(err, ErrEmptyAuthenticator) && !errors.Is(err, ErrMalformedToken) {
			t.Errorf("expected ErrEmptyAuthenticator for 98 bytes, got %v", err)
		}
	})

	t.Run("BinaryMarshalerInterface", func(t *testing.T) {
		t.Parallel()

		tok := &Token{
			TokenType:       TokenTypeBlindRSA,
			Nonce:           nonce,
			ChallengeDigest: challengeDigest,
			TokenKeyID:      tokenKeyID,
			Authenticator:   []byte{0x01, 0x02, 0x03},
		}

		data, err := tok.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary failed: %v", err)
		}

		var decoded Token
		if err := decoded.UnmarshalBinary(data); err != nil {
			t.Fatalf("UnmarshalBinary failed: %v", err)
		}

		if decoded.TokenType != tok.TokenType {
			t.Errorf("TokenType mismatch")
		}

		if !bytes.Equal(decoded.Authenticator, tok.Authenticator) {
			t.Errorf("Authenticator mismatch")
		}
	})
}

func TestWire_Concurrent(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 100
		iterations = 500
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()

			var (
				nonce           [32]byte
				challengeDigest [32]byte
				tokenKeyID      [32]byte
			)

			nonce[0] = byte(gID)
			challengeDigest[0] = byte(gID + 1)
			tokenKeyID[0] = byte(gID + 2)

			for i := 0; i < iterations; i++ {
				// 1. BuildTokenInput
				ti := BuildTokenInput(TokenTypeBlindRSA, nonce, challengeDigest, tokenKeyID)
				if ti[0] != 0x00 || ti[1] != 0x02 {
					t.Errorf("concurrent BuildTokenInput corrupt")
				}

				// 2. TokenChallenge
				tc := &TokenChallenge{
					TokenType:         TokenTypeBlindRSA,
					IssuerName:        "issuer.example.com",
					RedemptionContext: nil,
					OriginInfo:        "https://origin.com",
				}

				cWire, err := MarshalTokenChallenge(tc)
				if err != nil {
					t.Errorf("concurrent MarshalTokenChallenge failed: %v", err)
				}

				parsedTC, err := UnmarshalTokenChallenge(cWire)
				if err != nil || parsedTC.IssuerName != tc.IssuerName {
					t.Errorf("concurrent UnmarshalTokenChallenge failed: %v", err)
				}

				// 3. TokenRequest
				tr := &TokenRequest{
					TokenType:           TokenTypeBlindRSA,
					TruncatedTokenKeyID: byte(gID),
					BlindedMsg:          []byte{0x01, 0x02, 0x03, 0x04},
				}

				reqWire, err := MarshalTokenRequest(tr)
				if err != nil {
					t.Errorf("concurrent MarshalTokenRequest failed: %v", err)
				}

				parsedTR, err := UnmarshalTokenRequest(reqWire)
				if err != nil || parsedTR.TruncatedTokenKeyID != byte(gID) {
					t.Errorf("concurrent UnmarshalTokenRequest failed: %v", err)
				}

				// 4. Token
				tok := &Token{
					TokenType:       TokenTypeBlindRSA,
					Nonce:           nonce,
					ChallengeDigest: challengeDigest,
					TokenKeyID:      tokenKeyID,
					Authenticator:   []byte{0x10, 0x20, 0x30},
				}

				tokWire, err := MarshalToken(tok)
				if err != nil {
					t.Errorf("concurrent MarshalToken failed: %v", err)
				}

				parsedTok, err := UnmarshalToken(tokWire)
				if err != nil || parsedTok.Nonce[0] != byte(gID) {
					t.Errorf("concurrent UnmarshalToken failed: %v", err)
				}
			}
		}(g)
	}

	wg.Wait()
}

func BenchmarkBuildTokenInput_ZeroAlloc(b *testing.B) {
	var (
		tokenType       = TokenTypeBlindRSA
		nonce           [32]byte
		challengeDigest [32]byte
		tokenKeyID      [32]byte
	)
	copy(nonce[:], "sample-random-nonce-bytes-123456")
	copy(challengeDigest[:], "sample-challenge-digest-bytes-12")
	copy(tokenKeyID[:], "sample-token-key-id-bytes-123456")

	// Programmatic assertion: 0 heap allocations
	allocs := testing.AllocsPerRun(100, func() {
		_ = BuildTokenInput(tokenType, nonce, challengeDigest, tokenKeyID)
	})
	if allocs > 0 {
		b.Fatalf("expected 0 allocations, got %v", allocs)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = BuildTokenInput(tokenType, nonce, challengeDigest, tokenKeyID)
	}
}

func BenchmarkToken_TokenInput_ZeroAlloc(b *testing.B) {
	tok := &Token{
		TokenType: TokenTypeBlindRSA,
	}
	copy(tok.Nonce[:], "sample-random-nonce-bytes-123456")
	copy(tok.ChallengeDigest[:], "sample-challenge-digest-bytes-12")
	copy(tok.TokenKeyID[:], "sample-token-key-id-bytes-123456")

	allocs := testing.AllocsPerRun(100, func() {
		_ = tok.TokenInput()
	})
	if allocs > 0 {
		b.Fatalf("expected 0 allocations, got %v", allocs)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = tok.TokenInput()
	}
}

func BenchmarkUnmarshalTokenChallenge_SteadyState(b *testing.B) {
	wire := []byte{
		0x00, 0x02,
		0x00, 0x12,
		'i', 's', 's', 'u', 'e', 'r', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm',
		0x00,
		0x00, 0x00,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		tc, err := UnmarshalTokenChallenge(wire)
		if err != nil || tc == nil {
			b.Fatal("unexpected unmarshal failure")
		}
	}
}

func BenchmarkMarshalTokenChallenge(b *testing.B) {
	tc := &TokenChallenge{
		TokenType:  TokenTypeBlindRSA,
		IssuerName: "issuer.example.com",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		wire, err := MarshalTokenChallenge(tc)
		if err != nil || len(wire) == 0 {
			b.Fatal("unexpected marshal failure")
		}
	}
}

func BenchmarkUnmarshalToken(b *testing.B) {
	raw := make([]byte, 354)
	raw[0] = 0x00
	raw[1] = 0x02

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		tok, err := UnmarshalToken(raw)
		if err != nil || tok == nil {
			b.Fatal("unexpected unmarshal failure")
		}
	}
}

func BenchmarkMarshalToken(b *testing.B) {
	tok := &Token{
		TokenType:     TokenTypeBlindRSA,
		Authenticator: make([]byte, 256),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		wire, err := MarshalToken(tok)
		if err != nil || len(wire) == 0 {
			b.Fatal("unexpected marshal failure")
		}
	}
}
