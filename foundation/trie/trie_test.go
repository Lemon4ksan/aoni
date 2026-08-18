// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trie_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/trie"
)

func TestRadixTree_BasicOperations(t *testing.T) {
	t.Parallel()

	tree := trie.New[int]()
	assert.Equal(t, 0, tree.Len())

	// Insert
	_, updated := tree.Insert("apple", 1)
	assert.False(t, updated)
	assert.Equal(t, 1, tree.Len())

	_, updated = tree.Insert("app", 2)
	assert.False(t, updated)
	assert.Equal(t, 2, tree.Len())

	_, updated = tree.Insert("application", 3)
	assert.False(t, updated)
	assert.Equal(t, 3, tree.Len())

	// Overwrite
	old, updated := tree.Insert("app", 20)
	assert.True(t, updated)
	assert.Equal(t, 2, old)
	assert.Equal(t, 3, tree.Len())

	// Get
	val, ok := tree.Get("app")
	assert.True(t, ok)
	assert.Equal(t, 20, val)

	val, ok = tree.Get("apple")
	assert.True(t, ok)
	assert.Equal(t, 1, val)

	val, ok = tree.Get("application")
	assert.True(t, ok)
	assert.Equal(t, 3, val)

	_, ok = tree.Get("appl")
	assert.False(t, ok)

	_, ok = tree.Get("banana")
	assert.False(t, ok)
}

func TestRadixTree_LongestPrefix(t *testing.T) {
	t.Parallel()

	tree := trie.New[string]()
	tree.Insert("/", "root")
	tree.Insert("/api", "api_base")
	tree.Insert("/api/v1", "api_v1")
	tree.Insert("/api/v1/users", "users_endpoint")

	prefix, val, ok := tree.LongestPrefix("/api/v1/users/42/profile")
	assert.True(t, ok)
	assert.Equal(t, "/api/v1/users", prefix)
	assert.Equal(t, "users_endpoint", val)

	prefix, val, ok = tree.LongestPrefix("/api/v2/orders")
	assert.True(t, ok)
	assert.Equal(t, "/api", prefix)
	assert.Equal(t, "api_base", val)

	prefix, val, ok = tree.LongestPrefix("/healthz")
	assert.True(t, ok)
	assert.Equal(t, "/", prefix)
	assert.Equal(t, "root", val)

	_, _, ok = tree.LongestPrefix("nomatch")
	assert.False(t, ok)
}

func TestRadixTree_HasPrefix(t *testing.T) {
	t.Parallel()

	tree := trie.New[bool]()
	tree.Insert("GET /users", true)
	tree.Insert("POST /users", true)
	tree.Insert("GET /items/{id}", true)

	assert.True(t, tree.HasPrefix("GET /"))
	assert.True(t, tree.HasPrefix("POST /users"))
	assert.True(t, tree.HasPrefix("GET /items"))
	assert.False(t, tree.HasPrefix("DELETE /"))
	assert.False(t, tree.HasPrefix("GET /orders"))
}

func TestRadixTree_WalkPrefix(t *testing.T) {
	t.Parallel()

	tree := trie.New[int]()
	tree.Insert("route:user:get", 1)
	tree.Insert("route:user:post", 2)
	tree.Insert("route:user:delete", 3)
	tree.Insert("route:item:get", 4)

	var userMethods []string
	tree.WalkPrefix("route:user:", func(key string, val int) bool {
		userMethods = append(userMethods, key)
		return true
	})

	assert.Len(t, userMethods, 3)
	assert.Contains(t, userMethods, "route:user:get")
	assert.Contains(t, userMethods, "route:user:post")
	assert.Contains(t, userMethods, "route:user:delete")
}

func TestRadixTree_Delete(t *testing.T) {
	t.Parallel()

	tree := trie.New[string]()
	tree.Insert("foo", "1")
	tree.Insert("foobar", "2")
	tree.Insert("foobaz", "3")
	assert.Equal(t, 3, tree.Len())

	val, deleted := tree.Delete("foobar")
	assert.True(t, deleted)
	assert.Equal(t, "2", val)
	assert.Equal(t, 2, tree.Len())

	_, ok := tree.Get("foobar")
	assert.False(t, ok)

	val, ok = tree.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, "1", val)

	val, ok = tree.Get("foobaz")
	assert.True(t, ok)
	assert.Equal(t, "3", val)

	_, deleted = tree.Delete("non_existent")
	assert.False(t, deleted)
}

func BenchmarkRadixTree_Get_ZeroAlloc(b *testing.B) {
	tree := trie.New[int]()
	for i := 0; i < 1000; i++ {
		tree.Insert(fmt.Sprintf("/api/v1/service/%d/method", i), i)
	}

	searchKey := "/api/v1/service/500/method"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		val, ok := tree.Get(searchKey)
		if !ok || val != 500 {
			b.Fatalf("lookup failed")
		}
	}
}

func BenchmarkRadixTree_LongestPrefix_ZeroAlloc(b *testing.B) {
	tree := trie.New[string]()
	tree.Insert("/", "root")
	tree.Insert("/api", "api")
	tree.Insert("/api/v1", "v1")
	tree.Insert("/api/v1/users", "users")

	searchKey := "/api/v1/users/12345/details"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		prefix, val, ok := tree.LongestPrefix(searchKey)
		if !ok || prefix != "/api/v1/users" || val != "users" {
			b.Fatalf("prefix match failed")
		}
	}
}
