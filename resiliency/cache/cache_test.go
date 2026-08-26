// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/resiliency/cache"
)

func TestInMemoryStore_SetGetEviction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := cache.NewInMemoryStore(20 * time.Millisecond)
	t.Cleanup(store.Close)

	key := "user:123"
	val := []byte(`{"id":123,"name":"Test User"}`)

	// 1. Miss
	_, err := store.Get(ctx, key)
	assert.ErrorIs(t, err, cache.ErrCacheMiss)

	// 2. Set with 80ms TTL
	err = store.Set(ctx, key, val, 80*time.Millisecond)
	require.NoError(t, err)

	// 3. Hit via Get and GetOptional
	got, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, string(val), string(got))

	optVal := store.GetOptional(ctx, key)
	assert.True(t, optVal.IsPresent())
	unwrapped, ok := optVal.Value()
	assert.True(t, ok)
	assert.Equal(t, string(val), string(unwrapped))

	// 4. Expiration
	time.Sleep(120 * time.Millisecond)

	// 5. Miss after expiration
	_, err = store.Get(ctx, key)
	assert.ErrorIs(t, err, cache.ErrCacheMiss)
	assert.False(t, store.GetOptional(ctx, key).IsPresent())
}

func TestInMemoryStore_Overwrite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := cache.NewInMemoryStore(50 * time.Millisecond)
	t.Cleanup(store.Close)

	key := "config:flags"
	val1 := []byte("v1")
	val2 := []byte("v2")

	require.NoError(t, store.Set(ctx, key, val1, 200*time.Millisecond))

	got, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))

	// Overwrite key
	require.NoError(t, store.Set(ctx, key, val2, 200*time.Millisecond))

	gotUpdated, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "v2", string(gotUpdated))
}

func TestInMemoryStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := cache.NewInMemoryStore(10 * time.Millisecond)
	t.Cleanup(store.Close)

	const goroutines = 20

	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			key := "item"
			val := []byte("content")

			_ = store.Set(ctx, key, val, 50*time.Millisecond)
			_, _ = store.Get(ctx, key)
		}()
	}

	wg.Wait()
}

type sampleUser struct {
	ID   int
	Name string
}

func TestGenericStore_Typed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := cache.New[int, sampleUser](20 * time.Millisecond)
	t.Cleanup(store.Close)

	user := sampleUser{ID: 42, Name: "Alice"}

	// 1. Miss
	_, err := store.Get(ctx, 42)
	assert.ErrorIs(t, err, cache.ErrCacheMiss)

	// 2. Set with TTL
	err = store.Set(ctx, 42, user, 600*time.Millisecond)
	require.NoError(t, err)

	// 3. Hit
	got, err := store.Get(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, user, got)

	// 4. Hit via Optional
	opt := store.GetOptional(ctx, 42)
	assert.True(t, opt.IsPresent())
	assert.Equal(t, user, opt.MustValue())

	// 5. Expiration
	time.Sleep(700 * time.Millisecond)

	_, err = store.Get(ctx, 42)
	assert.ErrorIs(t, err, cache.ErrCacheMiss)
}

func TestShardedStore_SetGetEviction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := cache.NewShardedStore[string, []byte](20 * time.Millisecond)
	t.Cleanup(store.Close)

	key := "user:999"
	val := []byte(`{"id":999,"name":"Sharded User"}`)

	// Miss
	_, err := store.Get(ctx, key)
	assert.ErrorIs(t, err, cache.ErrCacheMiss)

	// Set with 80ms TTL
	err = store.Set(ctx, key, val, 80*time.Millisecond)
	require.NoError(t, err)

	// Hit via Get, GetDirect, and GetOptional
	got, err := store.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, string(val), string(got))

	gotDirect, err := store.GetDirect(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, val, gotDirect)

	opt := store.GetOptional(ctx, key)
	assert.True(t, opt.IsPresent())
	assert.Equal(t, string(val), string(opt.MustValue()))

	// Expiration
	time.Sleep(120 * time.Millisecond)

	_, err = store.Get(ctx, key)
	assert.ErrorIs(t, err, cache.ErrCacheMiss)
}

func BenchmarkInMemoryStore_Parallel(b *testing.B) {
	ctx := b.Context()

	store := cache.NewInMemoryStore(0)
	defer store.Close()

	val := []byte("cached-payload-content")
	_ = store.Set(ctx, "bench-key", val, time.Hour)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = store.Get(ctx, "bench-key")
		}
	})
}

func BenchmarkShardedStore_Parallel(b *testing.B) {
	ctx := b.Context()

	store := cache.NewShardedStore[string, []byte](0)
	defer store.Close()

	val := []byte("cached-payload-content")
	_ = store.Set(ctx, "bench-key", val, time.Hour)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = store.GetDirect(ctx, "bench-key")
		}
	})
}

func TestLRUStore(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := cache.NewLRUStore[string, []byte](2)

	// Set 2 items
	require.NoError(t, store.Set(ctx, "k1", []byte("v1"), time.Hour))
	require.NoError(t, store.Set(ctx, "k2", []byte("v2"), time.Hour))
	assert.Equal(t, 2, store.Len())

	// Access k1 to make k2 least recently used
	val, err := store.Get(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, "v1", string(val))

	// Insert k3 -> should evict k2
	require.NoError(t, store.Set(ctx, "k3", []byte("v3"), time.Hour))

	_, err = store.Get(ctx, "k2")
	assert.ErrorIs(t, err, cache.ErrCacheMiss)

	val, err = store.Get(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, "v1", string(val))

	val, err = store.Get(ctx, "k3")
	require.NoError(t, err)
	assert.Equal(t, "v3", string(val))

	// Delete k1
	require.NoError(t, store.Delete(ctx, "k1"))
	_, err = store.Get(ctx, "k1")
	assert.ErrorIs(t, err, cache.ErrCacheMiss)
}

func BenchmarkLRUStore_Parallel(b *testing.B) {
	ctx := b.Context()

	store := cache.NewLRUStore[string, []byte](1024)

	val := []byte("cached-payload-content")
	_ = store.Set(ctx, "bench-key", val, time.Hour)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = store.Get(ctx, "bench-key")
		}
	})
}
