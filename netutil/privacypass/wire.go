// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MarshalTokenChallenge encodes challenge into its RFC 9577 §2.1.1 TLS-presentation binary wire format.
func MarshalTokenChallenge(challenge *TokenChallenge) []byte {
	if challenge == nil {
		return nil
	}

	issuerLen := len(challenge.IssuerName)
	ctxLen := len(challenge.RedemptionContext)
	originLen := len(challenge.OriginInfo)

	// token_type(2) + issuer_len(2) + issuer + ctx_len(1) + ctx + origin_len(2) + origin
	totalLen := 2 + 2 + issuerLen + 1 + ctxLen + 2 + originLen
	buf := make([]byte, totalLen)

	binary.BigEndian.PutUint16(buf[0:2], uint16(challenge.TokenType))
	binary.BigEndian.PutUint16(buf[2:4], uint16(issuerLen))
	copy(buf[4:4+issuerLen], challenge.IssuerName)

	offset := 4 + issuerLen
	buf[offset] = byte(ctxLen)
	offset++

	if ctxLen > 0 {
		copy(buf[offset:offset+ctxLen], challenge.RedemptionContext)
		offset += ctxLen
	}

	binary.BigEndian.PutUint16(buf[offset:offset+2], uint16(originLen))
	offset += 2

	if originLen > 0 {
		copy(buf[offset:], challenge.OriginInfo)
	}

	return buf
}

// UnmarshalTokenChallenge parses an RFC 9577 §2.1.1 TokenChallenge from raw binary wire bytes.
func UnmarshalTokenChallenge(data []byte) (*TokenChallenge, error) {
	if len(data) < 7 { // min: 2 + 2 + 0 + 1 + 0 + 2 + 0 = 7
		return nil, ErrInvalidChallengeData
	}

	tokenType := TokenType(binary.BigEndian.Uint16(data[0:2]))
	issuerLen := int(binary.BigEndian.Uint16(data[2:4]))

	if len(data) < 4+issuerLen+1 {
		return nil, ErrInvalidChallengeData
	}

	issuerName := string(data[4 : 4+issuerLen])
	offset := 4 + issuerLen

	ctxLen := int(data[offset])
	offset++

	if ctxLen != 0 && ctxLen != 32 {
		return nil, ErrInvalidRedemptionContext
	}

	if len(data) < offset+ctxLen+2 {
		return nil, ErrInvalidChallengeData
	}

	var redemptionContext []byte
	if ctxLen > 0 {
		redemptionContext = make([]byte, ctxLen)
		copy(redemptionContext, data[offset:offset+ctxLen])
		offset += ctxLen
	}

	originLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	if len(data) < offset+originLen {
		return nil, ErrInvalidChallengeData
	}

	var originInfo string
	if originLen > 0 {
		originInfo = string(data[offset : offset+originLen])
	}

	return &TokenChallenge{
		TokenType:         tokenType,
		IssuerName:        issuerName,
		RedemptionContext: redemptionContext,
		OriginInfo:        originInfo,
	}, nil
}

// ComputeChallengeDigest computes SHA-256(TokenChallenge) as specified in RFC 9577 §2.2.1.
func ComputeChallengeDigest(challenge *TokenChallenge) [32]byte {
	raw := MarshalTokenChallenge(challenge)
	return sha256.Sum256(raw)
}

// MarshalToken serializes a [Token] into its RFC 9577 §2.2.1 binary representation.
func MarshalToken(t *Token) []byte {
	if t == nil {
		return nil
	}

	keyIDLen := len(t.TokenKeyID)
	authLen := len(t.Authenticator)

	// token_type(2) + nonce(32) + challenge_digest(32) + key_id(Nid) + auth(Nk)
	totalLen := 2 + 32 + 32 + keyIDLen + authLen
	buf := make([]byte, totalLen)

	binary.BigEndian.PutUint16(buf[0:2], uint16(t.TokenType))
	copy(buf[2:34], t.Nonce[:])
	copy(buf[34:66], t.ChallengeDigest[:])

	offset := 66
	if keyIDLen > 0 {
		copy(buf[offset:offset+keyIDLen], t.TokenKeyID)
		offset += keyIDLen
	}

	if authLen > 0 {
		copy(buf[offset:], t.Authenticator)
	}

	return buf
}

// UnmarshalToken parses an RFC 9577 §2.2.1 Token from raw binary bytes using fixed keyIDLen and authLen.
func UnmarshalToken(data []byte, keyIDLen, authLen int) (*Token, error) {
	expectedLen := 2 + 32 + 32 + keyIDLen + authLen
	if len(data) < expectedLen {
		return nil, ErrInvalidTokenData
	}

	t := &Token{
		TokenType: TokenType(binary.BigEndian.Uint16(data[0:2])),
	}

	copy(t.Nonce[:], data[2:34])
	copy(t.ChallengeDigest[:], data[34:66])

	offset := 66
	if keyIDLen > 0 {
		t.TokenKeyID = make([]byte, keyIDLen)
		copy(t.TokenKeyID, data[offset:offset+keyIDLen])
		offset += keyIDLen
	}

	if authLen > 0 {
		t.Authenticator = make([]byte, authLen)
		copy(t.Authenticator, data[offset:offset+authLen])
	}

	return t, nil
}

// ParseWWWAuthenticate parses all RFC 9577 PrivateToken challenges from a WWW-Authenticate header string.
func ParseWWWAuthenticate(header string) ([]*ChallengeParams, error) {
	if header == "" {
		return nil, nil
	}

	var results []*ChallengeParams

	// Challenges can be comma-separated or multiple PrivateToken challenges
	challenges := splitChallenges(header)

	for _, chStr := range challenges {
		chStr = strings.TrimSpace(chStr)
		if !strings.HasPrefix(strings.ToLower(chStr), strings.ToLower(SchemePrivateToken)) {
			continue
		}

		paramsStr := strings.TrimSpace(chStr[len(SchemePrivateToken):])
		params := parseAuthParams(paramsStr)

		challengeBase64, ok := params["challenge"]
		if !ok || challengeBase64 == "" {
			continue
		}

		challengeBytes, err := base64.URLEncoding.DecodeString(padBase64(challengeBase64))
		if err != nil {
			continue
		}

		tokenChallenge, err := UnmarshalTokenChallenge(challengeBytes)
		if err != nil {
			continue
		}

		cp := &ChallengeParams{
			Challenge: tokenChallenge,
			RawParam:  paramsStr,
		}

		if keyB64, ok := params["token-key"]; ok && keyB64 != "" {
			if keyBytes, err := base64.URLEncoding.DecodeString(padBase64(keyB64)); err == nil {
				cp.TokenKey = keyBytes
			}
		}

		if maxAgeStr, ok := params["max-age"]; ok && maxAgeStr != "" {
			if secs, err := strconv.ParseInt(maxAgeStr, 10, 64); err == nil && secs > 0 {
				cp.MaxAge = time.Duration(secs) * time.Second
			}
		}

		if realm, ok := params["realm"]; ok {
			cp.Realm = realm
		}

		results = append(results, cp)
	}

	return results, nil
}

// FormatAuthorizationToken formats tokenBytes into an RFC 9577 §2.2.2 `Authorization: PrivateToken token="..."` header.
func FormatAuthorizationToken(tokenBytes []byte) string {
	b64 := base64.URLEncoding.EncodeToString(tokenBytes)
	return fmt.Sprintf("%s token=\"%s\"", SchemePrivateToken, b64)
}

// FormatSecPrivateStateToken formats tokenBytes into a W3C `Sec-Private-State-Token` header.
func FormatSecPrivateStateToken(tokenBytes []byte) string {
	return base64.URLEncoding.EncodeToString(tokenBytes)
}

func padBase64(s string) string {
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}

	return s
}

func splitChallenges(h string) []string {
	var (
		list []string
		cur  strings.Builder
	)

	inQuote := false

	for i := 0; i < len(h); i++ {
		c := h[i]
		if c == '"' && (i == 0 || h[i-1] != '\\') {
			inQuote = !inQuote
		}

		if c == ',' && !inQuote {
			// Check if next token starts a new scheme
			rem := strings.TrimSpace(h[i+1:])
			if strings.HasPrefix(strings.ToLower(rem), strings.ToLower(SchemePrivateToken)) {
				list = append(list, cur.String())
				cur.Reset()
				continue
			}
		}

		cur.WriteByte(c)
	}

	if cur.Len() > 0 {
		list = append(list, cur.String())
	}

	return list
}

func parseAuthParams(s string) map[string]string {
	params := make(map[string]string)
	pairs := strings.Split(s, ",")

	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		k, v, found := strings.Cut(pair, "=")
		if !found {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.Trim(strings.TrimSpace(v), `"`)
		params[key] = val
	}

	return params
}
