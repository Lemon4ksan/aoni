// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// CachedToken represents a pre-fetched or stored Privacy Pass token with expiration.
type CachedToken struct {
	Token     *Token
	RawBytes  []byte
	ExpiresAt time.Time
}

// IsExpired reports whether the cached token has exceeded its validity window.
func (c *CachedToken) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}

	return time.Now().After(c.ExpiresAt)
}

// TokenCache provides thread-safe in-memory caching of Privacy Pass tokens per RFC 9577 §2.1.4.
// Tokens are strictly indexed by (token_type, issuer_name, redemption_context, origin_info).
type TokenCache struct {
	mu     sync.RWMutex
	tokens map[string][]*CachedToken
}

// NewTokenCache creates an empty, initialized [TokenCache].
func NewTokenCache() *TokenCache {
	return &TokenCache{
		tokens: make(map[string][]*CachedToken),
	}
}

// CacheKey computes the canonical cache key for challenge parameters (RFC 9577 §2.1.4).
func CacheKey(c *TokenChallenge) string {
	if c == nil {
		return ""
	}

	ctxHash := ""
	if len(c.RedemptionContext) > 0 {
		h := sha256.Sum256(c.RedemptionContext)
		ctxHash = hex.EncodeToString(h[:])
	}

	return fmt.Sprintf("%04x:%s:%s:%s", uint16(c.TokenType), c.IssuerName, ctxHash, c.OriginInfo)
}

// Put adds a token to the cache matching challenge with optional ttl.
func (tc *TokenCache) Put(challenge *TokenChallenge, token *Token, rawBytes []byte, ttl time.Duration) {
	if challenge == nil || token == nil {
		return
	}

	key := CacheKey(challenge)

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	ct := &CachedToken{
		Token:     token,
		RawBytes:  rawBytes,
		ExpiresAt: expiresAt,
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.tokens[key] = append(tc.tokens[key], ct)
}

// Get retrieves and removes one unexpired token matching challenge from the cache (single-use spend).
func (tc *TokenCache) Get(challenge *TokenChallenge) (*CachedToken, bool) {
	if challenge == nil {
		return nil, false
	}

	key := CacheKey(challenge)

	tc.mu.Lock()
	defer tc.mu.Unlock()

	list, ok := tc.tokens[key]
	if !ok || len(list) == 0 {
		return nil, false
	}

	now := time.Now()
	for i, ct := range list {
		if !ct.ExpiresAt.IsZero() && now.After(ct.ExpiresAt) {
			continue
		}

		// Remove the token from the slice (tokens MUST be spent only once per RFC 9577 §2)
		tc.tokens[key] = append(list[:i], list[i+1:]...)

		return ct, true
	}

	// All tokens were expired, clean up map entry
	delete(tc.tokens, key)

	return nil, false
}

// Flush removes all cached tokens across all challenges (e.g. on network or cookie reset per RFC 9577 §2.1.4).
func (tc *TokenCache) Flush() {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.tokens = make(map[string][]*CachedToken)
}

// Len returns the total number of cached tokens across all challenges.
func (tc *TokenCache) Len() int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	count := 0
	for _, list := range tc.tokens {
		count += len(list)
	}

	return count
}
