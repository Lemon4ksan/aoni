// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dict_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/dict"
)

func TestParseUseAsDictionary(t *testing.T) {
	respURL, err := url.Parse("https://example.org/dict/schema.json")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Standard parameters", func(t *testing.T) {
		hdr := `match="/product/*", match-dest=("document"), id="dict-12345", type=raw, ttl=86400`

		meta, err := dict.ParseUseAsDictionary(hdr, respURL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if meta.Match != "/product/*" {
			t.Errorf("expected match='/product/*', got %q", meta.Match)
		}

		if len(meta.MatchDest) != 1 || meta.MatchDest[0] != "document" {
			t.Errorf("expected match-dest=['document'], got %v", meta.MatchDest)
		}

		if meta.ID != "dict-12345" {
			t.Errorf("expected ID='dict-12345', got %q", meta.ID)
		}

		if meta.Type != dict.TypeRaw {
			t.Errorf("expected Type='raw', got %q", meta.Type)
		}

		if meta.TTL != 86400*time.Second {
			t.Errorf("expected TTL=86400s, got %v", meta.TTL)
		}
	})

	t.Run("Versioned directory pattern", func(t *testing.T) {
		hdr := `match="/app/*/main.js"`

		meta, err := dict.ParseUseAsDictionary(hdr, respURL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if meta.Match != "/app/*/main.js" {
			t.Errorf("expected match='/app/*/main.js', got %q", meta.Match)
		}

		if meta.Type != dict.TypeRaw {
			t.Errorf("expected default Type='raw', got %q", meta.Type)
		}
	})

	t.Run("Unsupported type rejection", func(t *testing.T) {
		hdr := `match="/api/*", type=unknown-codec`

		_, err := dict.ParseUseAsDictionary(hdr, respURL)
		if err == nil {
			t.Fatal("expected error for unsupported type, got nil")
		}
	})
}

func TestFormatAndParseAvailableDictionary(t *testing.T) {
	data := []byte("schema dictionary v1 payload")
	hash := dict.ComputeSHA256(data)

	formatted := dict.FormatAvailableDictionary(hash)
	if len(formatted) == 0 || formatted[0] != ':' || formatted[len(formatted)-1] != ':' {
		t.Fatalf("invalid formatted byte sequence: %s", formatted)
	}

	parsedHash, err := dict.ParseAvailableDictionary(formatted)
	if err != nil {
		t.Fatalf("failed to parse formatted Available-Dictionary: %v", err)
	}

	if parsedHash != hash {
		t.Fatalf("hash mismatch: expected %x, got %x", hash, parsedHash)
	}
}

func TestURLPatternMatching(t *testing.T) {
	baseURL, _ := url.Parse("https://example.org/api/v1/dict")
	targetURL1, _ := url.Parse("https://example.org/product/shoes/nike")
	targetURL2, _ := url.Parse("https://example.org/app/v2/main.js")
	targetCross, _ := url.Parse("https://other.org/product/shoes/nike")

	// Same origin wildcard
	if !dict.MatchURLPattern("/product/*", baseURL, targetURL1) {
		t.Error("expected match for /product/* on targetURL1")
	}

	if !dict.MatchURLPattern("/app/*/main.js", baseURL, targetURL2) {
		t.Error("expected match for /app/*/main.js on targetURL2")
	}

	if dict.MatchURLPattern("/app/*/other.js", baseURL, targetURL2) {
		t.Error("did not expect match for /app/*/other.js")
	}

	// Cross-origin must fail
	if dict.IsSameOrigin(baseURL, targetCross) {
		t.Error("expected IsSameOrigin to be false for cross-origin URLs")
	}
}

func TestStorePrecedenceAndEviction(t *testing.T) {
	store := dict.NewStore(
		dict.WithMaxBytes(1024),
		dict.WithDefaultTTL(1*time.Hour),
	)

	u1, _ := url.Parse("https://api.example.com/dict1")
	u2, _ := url.Parse("https://api.example.com/dict2")
	u3, _ := url.Parse("https://api.example.com/dict3")

	target, _ := url.Parse("https://api.example.com/v1/users/list")

	// Store Dict 1: General match "/v1/*"
	d1, err := store.Store(u1, `match="/v1/*", id="dict-general"`, []byte("general dict 1"))
	if err != nil {
		t.Fatal(err)
	}

	// Store Dict 2: Specific match with destination "/v1/users/*", match-dest=("document")
	d2, err := store.Store(u2, `match="/v1/users/*", match-dest=("document"), id="dict-dest"`, []byte("dest dict 2"))
	if err != nil {
		t.Fatal(err)
	}

	// Store Dict 3: Longer match without destination "/v1/users/list"
	d3, err := store.Store(u3, `match="/v1/users/list", id="dict-exact"`, []byte("exact dict 3"))
	if err != nil {
		t.Fatal(err)
	}

	// 1. Destination request -> should pick d2 (Rule 1: match-dest precedence)
	matched, ok := store.Match(target, "document")
	if !ok || matched.ID != d2.ID {
		t.Fatalf("expected to match d2 (dest precedence), got %v", matched)
	}

	// 2. No destination request -> should pick d3 (Rule 2: longest match "/v1/users/list")
	matched, ok = store.Match(target, "")
	if !ok || matched.ID != d3.ID {
		t.Fatalf("expected to match d3 (longest match precedence), got %v", matched)
	}

	// 3. Lookup by hash & ID
	if byHash, ok := store.GetByHash(d1.Hash); !ok || byHash.ID != d1.ID {
		t.Fatalf("failed to retrieve by hash: %v", byHash)
	}

	if byID, ok := store.GetByID("dict-general"); !ok || byID.Hash != d1.Hash {
		t.Fatalf("failed to retrieve by ID: %v", byID)
	}

	// 4. Memory eviction
	largeData := make([]byte, 800)
	_, _ = store.Store(u1, `match="/big/*"`, largeData)
	// Should have evicted older entries to stay under 1024 bytes
	if store.Bytes() > 1024 {
		t.Fatalf("store memory limit exceeded: %d > 1024", store.Bytes())
	}
}

func TestDictionary_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("freshness_checks", func(t *testing.T) {
		t.Parallel()

		var nilDict *dict.Dictionary
		assert.False(t, nilDict.IsFresh(time.Now()))

		zeroExpiryDict := &dict.Dictionary{}
		assert.True(t, zeroExpiryDict.IsFresh(time.Now()))

		futureExpiry := &dict.Dictionary{ExpiresAt: time.Now().Add(time.Hour)}
		assert.True(t, futureExpiry.IsFresh(time.Now()))

		pastExpiry := &dict.Dictionary{ExpiresAt: time.Now().Add(-time.Hour)}
		assert.False(t, pastExpiry.IsFresh(time.Now()))
	})

	t.Run("matches_nil_and_dest_predicates", func(t *testing.T) {
		t.Parallel()

		var nilDict *dict.Dictionary
		assert.False(t, nilDict.Matches(nil, ""))

		baseURL, _ := url.Parse("https://example.com/dir/file")
		d := &dict.Dictionary{
			BaseURL:   baseURL,
			Match:     "/dir/*",
			MatchDest: []string{"document", "script"},
		}

		assert.False(t, d.Matches(nil, "document"))
		assert.True(t, dict.MatchDest(nil, "any"))
		assert.False(t, dict.MatchDest([]string{"document"}, ""))
		assert.True(t, dict.MatchDest([]string{"document"}, "DOCUMENT"))
		assert.False(t, dict.MatchDest([]string{"document"}, "image"))
	})

	t.Run("same_origin_edge", func(t *testing.T) {
		t.Parallel()

		assert.False(t, dict.IsSameOrigin(nil, nil))

		u1, _ := url.Parse("https://example.com:443/a")
		u2, _ := url.Parse("http://example.com:443/b")
		assert.False(t, dict.IsSameOrigin(u1, u2))

		u3, _ := url.Parse("https://example.com:8443/a")
		assert.False(t, dict.IsSameOrigin(u1, u3))
	})

	t.Run("parse_use_as_dictionary_errors", func(t *testing.T) {
		t.Parallel()

		_, err := dict.ParseUseAsDictionary("", nil)
		assert.ErrorIs(t, err, dict.ErrInvalidUseAsDictionary)

		_, err = dict.ParseUseAsDictionary("match", nil)
		assert.ErrorIs(t, err, dict.ErrInvalidUseAsDictionary)

		_, err = dict.ParseUseAsDictionary("id=\"test\"", nil)
		assert.ErrorIs(t, err, dict.ErrMissingMatchPattern)

		respURL, _ := url.Parse("https://example.com/api/v1/resource")
		meta, err := dict.ParseUseAsDictionary("id=\"test\"", respURL)
		require.NoError(t, err)
		assert.Equal(t, "/api/v1/*", meta.Match)
	})

	t.Run("parse_available_dictionary_errors", func(t *testing.T) {
		t.Parallel()

		_, err := dict.ParseAvailableDictionary("not_bracketed")
		assert.ErrorIs(t, err, dict.ErrInvalidAvailableDictionary)

		_, err = dict.ParseAvailableDictionary(":short:")
		assert.ErrorIs(t, err, dict.ErrInvalidAvailableDictionary)

		_, err = dict.ParseAvailableDictionary(":invalid_base64_symbols!!!:")
		assert.ErrorIs(t, err, dict.ErrInvalidAvailableDictionary)
	})
}
