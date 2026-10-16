// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// TestConcurrency_M1_2_AllWorkloads stress-tests all Milestone 1 wire, parsing,
// and formatting operations across 128 parallel goroutines running thousands of operations each
// under the Go race detector.
func TestConcurrency_M1_2_AllWorkloads(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 128
		iterations = 1000
	)

	var (
		baseNonce           [NonceSize]byte
		baseChallengeDigest [ChallengeDigestSize]byte
		baseTokenKeyID      [TokenKeyIDSize]byte
	)
	for i := range baseNonce {
		baseNonce[i] = byte(i + 1)
		baseChallengeDigest[i] = byte(i + 33)
		baseTokenKeyID[i] = byte(i + 65)
	}

	auth := make([]byte, 256)
	for i := range auth {
		auth[i] = 0x55
	}

	sharedTok := &Token{
		TokenType:       TokenTypeBlindRSA,
		Nonce:           baseNonce,
		ChallengeDigest: baseChallengeDigest,
		TokenKeyID:      baseTokenKeyID,
		Authenticator:   auth,
	}

	rc := make([]byte, 32)
	for i := range rc {
		rc[i] = byte(i + 7)
	}

	sharedChallenge := &TokenChallenge{
		TokenType:         TokenTypeBlindRSA,
		IssuerName:        "issuer.stress.test.example.com",
		RedemptionContext: rc,
		OriginInfo:        "https://api.stress.test.example.com",
	}

	sharedChallengeWire, err := MarshalTokenChallenge(sharedChallenge)
	if err != nil {
		t.Fatalf("MarshalTokenChallenge setup failed: %v", err)
	}

	sharedHeader := fmt.Sprintf(
		`PrivateToken challenge="%s", max-age=600, token-key="dGVzdA"`,
		EncodeBase64URL(sharedChallengeWire),
	)

	// Workload 1: BuildTokenInput and (*Token).TokenInput()
	t.Run("Parallel_BuildTokenInput_And_TokenInput", func(t *testing.T) {
		t.Parallel()

		var wg sync.WaitGroup
		wg.Add(goroutines)

		var successCount atomic.Int64

		for g := 0; g < goroutines; g++ {
			go func(gID int) {
				defer wg.Done()

				localNonce := baseNonce
				localNonce[0] = byte(gID)

				for i := 0; i < iterations; i++ {
					// 1. Direct BuildTokenInput
					ti1 := BuildTokenInput(TokenTypeBlindRSA, localNonce, baseChallengeDigest, baseTokenKeyID)
					if ti1[0] != 0x00 || ti1[1] != 0x02 || ti1[2] != byte(gID) {
						t.Errorf("goroutine %d: BuildTokenInput corrupt: %x", gID, ti1[:4])
						return
					}

					// 2. Token.TokenInput() on shared struct (concurrent read)
					ti2 := sharedTok.TokenInput()
					if ti2[0] != 0x00 || ti2[1] != 0x02 || ti2[2] != baseNonce[0] {
						t.Errorf("goroutine %d: shared TokenInput corrupt", gID)
						return
					}

					// 3. Token.TokenInput() on locally created struct
					localTok := &Token{
						TokenType:       TokenTypeVOPRF,
						Nonce:           localNonce,
						ChallengeDigest: baseChallengeDigest,
						TokenKeyID:      baseTokenKeyID,
					}

					ti3 := localTok.TokenInput()
					if ti3[0] != 0x00 || ti3[1] != 0x01 || ti3[2] != byte(gID) {
						t.Errorf("goroutine %d: local TokenInput corrupt", gID)
						return
					}

					successCount.Add(1)
				}
			}(g)
		}

		wg.Wait()

		expected := int64(goroutines * iterations)
		if successCount.Load() != expected {
			t.Fatalf("expected %d iterations, got %d", expected, successCount.Load())
		}
	})

	// Workload 2: UnmarshalTokenChallenge and MarshalTokenChallenge
	t.Run("Parallel_TokenChallenge_MarshalUnmarshal", func(t *testing.T) {
		t.Parallel()

		var wg sync.WaitGroup
		wg.Add(goroutines)

		var successCount atomic.Int64

		for g := 0; g < goroutines; g++ {
			go func(gID int) {
				defer wg.Done()

				for i := 0; i < iterations; i++ {
					// 1. Unmarshal shared wire buffer concurrently
					tc, err := UnmarshalTokenChallenge(sharedChallengeWire)
					if err != nil {
						t.Errorf("goroutine %d: UnmarshalTokenChallenge failed: %v", gID, err)
						return
					}

					if tc.IssuerName != sharedChallenge.IssuerName {
						t.Errorf("goroutine %d: IssuerName mismatch: %s", gID, tc.IssuerName)
						return
					}

					// 2. Marshal shared challenge struct concurrently
					wireA, err := MarshalTokenChallenge(sharedChallenge)
					if err != nil || !bytes.Equal(wireA, sharedChallengeWire) {
						t.Errorf("goroutine %d: MarshalTokenChallenge(shared) failed: %v", gID, err)
						return
					}

					// 3. Marshal unmarshaled challenge struct
					wireB, err := MarshalTokenChallenge(tc)
					if err != nil || !bytes.Equal(wireB, sharedChallengeWire) {
						t.Errorf("goroutine %d: MarshalTokenChallenge(tc) failed: %v", gID, err)
						return
					}

					// 4. Thread-local challenge manipulation
					localIssuer := fmt.Sprintf("tenant-%d.example.com", (gID*1000)+i)
					localC := &TokenChallenge{
						TokenType:  TokenTypeVOPRF,
						IssuerName: localIssuer,
					}

					localWire, err := MarshalTokenChallenge(localC)
					if err != nil {
						t.Errorf("goroutine %d: local marshal failed: %v", gID, err)
						return
					}

					parsedLocal, err := UnmarshalTokenChallenge(localWire)
					if err != nil || parsedLocal.IssuerName != localIssuer {
						t.Errorf("goroutine %d: local unmarshal failed: %v", gID, err)
						return
					}

					successCount.Add(1)
				}
			}(g)
		}

		wg.Wait()

		expected := int64(goroutines * iterations)
		if successCount.Load() != expected {
			t.Fatalf("expected %d iterations, got %d", expected, successCount.Load())
		}
	})

	// Workload 3: ParseChallenge and FormatAuthorization
	t.Run("Parallel_ParseChallenge_And_FormatAuthorization", func(t *testing.T) {
		t.Parallel()

		var wg sync.WaitGroup
		wg.Add(goroutines)

		var successCount atomic.Int64

		for g := 0; g < goroutines; g++ {
			go func(gID int) {
				defer wg.Done()

				localAuth := make([]byte, 256)
				localAuth[0] = byte(gID)

				localTok := &Token{
					TokenType:       TokenTypeBlindRSA,
					Nonce:           baseNonce,
					ChallengeDigest: baseChallengeDigest,
					TokenKeyID:      baseTokenKeyID,
					Authenticator:   localAuth,
				}

				for i := 0; i < iterations; i++ {
					// 1. ParseChallenge on shared header string
					tc, err := ParseChallenge(sharedHeader)
					if err != nil {
						t.Errorf("goroutine %d: ParseChallenge failed: %v", gID, err)
						return
					}

					if tc.IssuerName != sharedChallenge.IssuerName {
						t.Errorf("goroutine %d: parsed IssuerName mismatch: %s", gID, tc.IssuerName)
						return
					}

					// 2. FormatAuthorization on shared token
					sharedAuthHdr := FormatAuthorization(sharedTok)
					if len(sharedAuthHdr) == 0 {
						t.Errorf("goroutine %d: FormatAuthorization(sharedTok) empty", gID)
						return
					}

					// 3. FormatAuthorization on local token & ParseAuthorization roundtrip
					localAuthHdr := FormatAuthorization(localTok)

					parsedTok, err := ParseAuthorization(localAuthHdr)
					if err != nil {
						t.Errorf("goroutine %d: ParseAuthorization failed: %v", gID, err)
						return
					}

					if parsedTok.Authenticator[0] != byte(gID) {
						t.Errorf(
							"goroutine %d: parsed Authenticator[0] = %d, want %d",
							gID,
							parsedTok.Authenticator[0],
							gID,
						)

						return
					}

					// 4. FormatAuthorizationStrict check
					strictHdr, err := FormatAuthorizationStrict(localTok)
					if err != nil || strictHdr != localAuthHdr {
						t.Errorf("goroutine %d: FormatAuthorizationStrict failed: %v", gID, err)
						return
					}

					// 5. Fast-path predicate check
					if !IsPrivateTokenChallenge(sharedHeader) {
						t.Errorf("goroutine %d: IsPrivateTokenChallenge returned false", gID)
						return
					}

					successCount.Add(1)
				}
			}(g)
		}

		wg.Wait()

		expected := int64(goroutines * iterations)
		if successCount.Load() != expected {
			t.Fatalf("expected %d iterations, got %d", expected, successCount.Load())
		}
	})
}

// TestZeroAlloc_M1_2_BuildTokenInput_Strict verifies that BuildTokenInput and
// (*Token).TokenInput() strictly incur 0 heap allocations across repeated runs.
func TestZeroAlloc_M1_2_BuildTokenInput_Strict(t *testing.T) {
	var (
		nonce           [NonceSize]byte
		challengeDigest [ChallengeDigestSize]byte
		tokenKeyID      [TokenKeyIDSize]byte
	)
	for i := range nonce {
		nonce[i] = byte(i)
		challengeDigest[i] = byte(i + 32)
		tokenKeyID[i] = byte(i + 64)
	}

	tok := &Token{
		TokenType:       TokenTypeBlindRSA,
		Nonce:           nonce,
		ChallengeDigest: challengeDigest,
		TokenKeyID:      tokenKeyID,
	}

	t.Run("BuildTokenInput_ZeroAllocs", func(t *testing.T) {
		allocs := testing.AllocsPerRun(1000, func() {
			out := BuildTokenInput(TokenTypeBlindRSA, nonce, challengeDigest, tokenKeyID)
			if out[0] != 0x00 || out[1] != 0x02 {
				t.Fatal("unexpected token type")
			}
		})
		if allocs != 0 {
			t.Fatalf("BuildTokenInput allocated %f times, expected exactly 0", allocs)
		}
	})

	t.Run("Token_TokenInput_ZeroAllocs", func(t *testing.T) {
		allocs := testing.AllocsPerRun(1000, func() {
			out := tok.TokenInput()
			if out[0] != 0x00 || out[1] != 0x02 {
				t.Fatal("unexpected token type")
			}
		})
		if allocs != 0 {
			t.Fatalf("Token.TokenInput allocated %f times, expected exactly 0", allocs)
		}
	})

	t.Run("BuildTokenInput_DigestHashing_ZeroAllocs", func(t *testing.T) {
		// When passed directly into sha256 or sha512 on the stack:
		allocs := testing.AllocsPerRun(1000, func() {
			ti := BuildTokenInput(TokenTypeBlindRSA, nonce, challengeDigest, tokenKeyID)

			digest := sha256.Sum256(ti[:])
			if digest[0] == 0xFF && digest[1] == 0xFF {
				t.Fatal("unlikely match")
			}
		})
		if allocs != 0 {
			t.Fatalf("BuildTokenInput + sha256 allocated %f times, expected exactly 0", allocs)
		}
	})
}

// TestStructuralIdentity_M1_2 ensures RFC 9577 / RFC 9578 invariant that
// the first 98 bytes of serialized Token is byte-for-byte identical to TokenInput preimage.
func TestStructuralIdentity_M1_2(t *testing.T) {
	for i := 0; i < 500; i++ {
		var (
			nonce [NonceSize]byte
			cd    [ChallengeDigestSize]byte
			tk    [TokenKeyIDSize]byte
		)

		_, _ = rand.Read(nonce[:])
		_, _ = rand.Read(cd[:])
		_, _ = rand.Read(tk[:])

		tt := TokenTypeBlindRSA
		if i%2 == 1 {
			tt = TokenTypeVOPRF
		}

		tok := &Token{
			TokenType:       tt,
			Nonce:           nonce,
			ChallengeDigest: cd,
			TokenKeyID:      tk,
			Authenticator:   []byte("test-authenticator-data"),
		}

		preimage := BuildTokenInput(tt, nonce, cd, tk)

		tokPreimage := tok.TokenInput()
		if preimage != tokPreimage {
			t.Fatalf("preimage mismatch on iteration %d", i)
		}

		wire, err := MarshalToken(tok)
		if err != nil {
			t.Fatalf("MarshalToken failed on iteration %d: %v", i, err)
		}

		if !bytes.Equal(wire[:TokenInputSize], preimage[:]) {
			t.Fatalf("wire prefix mismatch on iteration %d", i)
		}
	}
}
