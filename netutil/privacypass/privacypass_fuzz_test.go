// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/privacypass"
)

func FuzzUnmarshalTokenChallenge(f *testing.F) {
	seedValid := privacypass.MarshalTokenChallenge(&privacypass.TokenChallenge{
		TokenType:         privacypass.TypeBlindRSA,
		IssuerName:        "issuer.example.com",
		RedemptionContext: make([]byte, 32),
		OriginInfo:        "origin.example.com",
	})
	f.Add(seedValid)
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		ch, err := privacypass.UnmarshalTokenChallenge(data)
		if err == nil && ch != nil {
			_ = privacypass.ValidateChallenge("issuer.example.com", ch)
			_ = privacypass.ComputeChallengeDigest(ch)

			encoded := privacypass.MarshalTokenChallenge(ch)
			if len(encoded) == 0 {
				t.Fatalf("expected non-empty marshaled challenge")
			}
		}
	})
}

func FuzzUnmarshalToken(f *testing.F) {
	seedToken := privacypass.MarshalToken(&privacypass.Token{
		TokenType:     privacypass.TypeBlindRSA,
		TokenKeyID:    []byte{0x01, 0x02, 0x03, 0x04},
		Authenticator: []byte{0x05, 0x06, 0x07, 0x08},
	})
	f.Add(seedToken, 4, 4)
	f.Add([]byte{}, 0, 0)
	f.Add([]byte{0x00, 0x01}, -1, -5)
	f.Add([]byte("random binary payload that might be long"), 10, 20)

	f.Fuzz(func(t *testing.T, data []byte, keyIDLen, authLen int) {
		tok, err := privacypass.UnmarshalToken(data, keyIDLen, authLen)
		if err == nil && tok != nil {
			marshaled := privacypass.MarshalToken(tok)
			_ = privacypass.FormatAuthorizationToken(marshaled)
			_ = privacypass.FormatSecPrivateStateToken(marshaled)
		}
	})
}

func FuzzParseWWWAuthenticate(f *testing.F) {
	f.Add(
		`PrivateToken challenge="AQIABWlzc3VlcgAgAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABm9yaWdpbg==", token-key="AQIDBA==", max-age=3600`,
	)
	f.Add(`PrivateToken challenge="invalid_b64", realm="api"`)
	f.Add(`Bearer realm="oauth", PrivateToken challenge=""`)
	f.Add("")

	f.Fuzz(func(t *testing.T, header string) {
		challenges, err := privacypass.ParseWWWAuthenticate(header)
		if err == nil {
			for _, ch := range challenges {
				_ = ch.RawParam
				_ = ch.Realm
				_ = ch.MaxAge
			}
		}
	})
}
