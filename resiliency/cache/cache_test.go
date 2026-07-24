// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/resiliency/cache"
)

func TestInMemoryStore_SetGetEviction(t *testing.T) {
	ctx := context.Background()
	store := cache.NewInMemoryStore(50 * time.Millisecond)

	key := "user:123"
	val := []byte(`{"id":123,"name":"Test User"}`)

	// Miss
	if _, err := store.Get(ctx, key); err != cache.ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss, got %v", err)
	}

	// Set with 100ms TTL
	if err := store.Set(ctx, key, val, 100*time.Millisecond); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Hit
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got) != string(val) {
		t.Errorf("got %q, want %q", string(got), string(val))
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Miss after expiration
	if _, err := store.Get(ctx, key); err != cache.ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss after expiration, got %v", err)
	}

	store.Close()
}
