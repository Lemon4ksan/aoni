// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"context"
	"sync"
	"time"
)

// TokenProvider defines the client contract for obtaining valid Privacy Pass tokens for challenges.
type TokenProvider interface {
	// ProvideToken acquires or generates a Privacy Pass token corresponding to challenge for redemption at origin.
	ProvideToken(ctx context.Context, origin string, challenge *ChallengeParams) (tokenBytes []byte, err error)
}

// TokenFetchFunc is a function signature for acquiring tokens dynamically from an Attester/Issuer.
type TokenFetchFunc func(ctx context.Context, origin string, challenge *ChallengeParams) ([]byte, error)

// CallbackTokenProvider implements [TokenProvider] using a custom fetch function.
type CallbackTokenProvider struct {
	fn TokenFetchFunc
}

// NewCallbackProvider creates a [TokenProvider] backed by fn.
func NewCallbackProvider(fn TokenFetchFunc) *CallbackTokenProvider {
	return &CallbackTokenProvider{fn: fn}
}

// ProvideToken invokes the underlying callback function to acquire a token.
func (p *CallbackTokenProvider) ProvideToken(
	ctx context.Context,
	origin string,
	challenge *ChallengeParams,
) ([]byte, error) {
	if p.fn == nil {
		return nil, ErrNoTokenAvailable
	}

	return p.fn(ctx, origin, challenge)
}

// StaticTokenProvider implements [TokenProvider] returning pre-configured static token bytes.
type StaticTokenProvider struct {
	mu     sync.Mutex
	tokens map[string][][]byte
}

// NewStaticProvider creates a [StaticTokenProvider] initialized with static token mappings.
func NewStaticProvider() *StaticTokenProvider {
	return &StaticTokenProvider{
		tokens: make(map[string][][]byte),
	}
}

// AddToken registers tokenBytes for challenge.
func (p *StaticTokenProvider) AddToken(challenge *TokenChallenge, tokenBytes []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := CacheKey(challenge)
	p.tokens[key] = append(p.tokens[key], tokenBytes)
}

// ProvideToken retrieves one static token for challenge.
func (p *StaticTokenProvider) ProvideToken(_ context.Context, _ string, challenge *ChallengeParams) ([]byte, error) {
	if challenge == nil || challenge.Challenge == nil {
		return nil, ErrNoTokenAvailable
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := CacheKey(challenge.Challenge)

	list, ok := p.tokens[key]
	if !ok || len(list) == 0 {
		return nil, ErrNoTokenAvailable
	}

	token := list[0]
	p.tokens[key] = list[1:]

	return token, nil
}

// CachedTokenProvider wraps a [TokenCache] and a fallback [TokenProvider] to serve tokens with zero issuance latency.
type CachedTokenProvider struct {
	cache      *TokenCache
	fallback   TokenProvider
	defaultTTL time.Duration
}

// NewCachedProvider instantiates a [CachedTokenProvider] wrapping fallback with cache.
func NewCachedProvider(cache *TokenCache, fallback TokenProvider, defaultTTL time.Duration) *CachedTokenProvider {
	if cache == nil {
		cache = NewTokenCache()
	}

	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}

	return &CachedTokenProvider{
		cache:      cache,
		fallback:   fallback,
		defaultTTL: defaultTTL,
	}
}

// Cache returns the underlying [*TokenCache].
func (p *CachedTokenProvider) Cache() *TokenCache {
	return p.cache
}

// ProvideToken first attempts to retrieve an unspent token from cache; if missing, delegates to fallback.
func (p *CachedTokenProvider) ProvideToken(
	ctx context.Context,
	origin string,
	challenge *ChallengeParams,
) ([]byte, error) {
	if challenge != nil && challenge.Challenge != nil {
		if ct, ok := p.cache.Get(challenge.Challenge); ok {
			return ct.RawBytes, nil
		}
	}

	if p.fallback == nil {
		return nil, ErrNoTokenAvailable
	}

	tokenBytes, err := p.fallback.ProvideToken(ctx, origin, challenge)
	if err != nil {
		return nil, err
	}

	return tokenBytes, nil
}
