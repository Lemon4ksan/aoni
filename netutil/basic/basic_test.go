// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package basic_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/netutil/basic"
)

func TestBasicAuth_FormatAndParse_RFC7617(t *testing.T) {
	t.Parallel()

	// Short credentials (stack buffer optimization <= 128 bytes)
	shortAuth := basic.Format("Aladdin", "open sesame")
	assert.Equal(t, "Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==", shortAuth)

	u, p, ok := basic.Parse(shortAuth)
	assert.True(t, ok)
	assert.Equal(t, "Aladdin", u)
	assert.Equal(t, "open sesame", p)

	// Long credentials (> 128 bytes)
	longUser := strings.Repeat("u", 100)
	longPass := strings.Repeat("p", 100)
	longAuth := basic.Format(longUser, longPass)
	assert.True(t, strings.HasPrefix(longAuth, "Basic "))

	u2, p2, ok2 := basic.Parse(longAuth)
	assert.True(t, ok2)
	assert.Equal(t, longUser, u2)
	assert.Equal(t, longPass, p2)

	// Invalid header format
	_, _, okBad1 := basic.Parse("Bearer token")
	assert.False(t, okBad1)

	_, _, okBad2 := basic.Parse("Basic invalid-base64!!!")
	assert.False(t, okBad2)

	_, _, okBad3 := basic.Parse("Basic " + "bm9jb2xvbg==") // "nocolon"
	assert.False(t, okBad3)
}

func TestBasicAuth_ParseChallenge_RFC7617(t *testing.T) {
	t.Parallel()

	// RFC 7617 §2 example: Basic realm="WallyWorld", charset="UTF-8"
	ch1, ok1 := basic.ParseChallenge(`Basic realm="WallyWorld", charset="UTF-8"`)
	assert.True(t, ok1)
	assert.Equal(t, "WallyWorld", ch1.Realm)
	assert.Equal(t, "UTF-8", ch1.Charset)
	assert.Equal(t, `Basic realm="WallyWorld", charset="UTF-8"`, ch1.String())

	// Simple realm
	ch2, ok2 := basic.ParseChallenge(`Basic realm="Simple"`)
	assert.True(t, ok2)
	assert.Equal(t, "Simple", ch2.Realm)
	assert.Empty(t, ch2.Charset)
	assert.Equal(t, `Basic realm="Simple"`, ch2.String())

	// Case-insensitivity in scheme
	ch3, ok3 := basic.ParseChallenge(`basic realm="Secure"`)
	assert.True(t, ok3)
	assert.Equal(t, "Secure", ch3.Realm)

	// Invalid scheme
	_, okBad := basic.ParseChallenge(`Digest realm="Test"`)
	assert.False(t, okBad)
}

func TestBasicAuth_InScope_RFC7617(t *testing.T) {
	t.Parallel()

	scopeRoot := "https://example.com/api/v1"

	// Child paths are in scope
	assert.True(t, basic.InScope("https://example.com/api/v1/users", scopeRoot))
	assert.True(t, basic.InScope("https://example.com/api/v1/", scopeRoot))
	assert.True(t, basic.InScope("https://example.com/api/v1", scopeRoot))

	// Parent or sibling paths are NOT in scope
	assert.False(t, basic.InScope("https://example.com/api/v2", scopeRoot))
	assert.False(t, basic.InScope("https://example.com/other", scopeRoot))
	assert.False(t, basic.InScope("https://evil.com/api/v1/users", scopeRoot))
	assert.False(t, basic.InScope("http://example.com/api/v1/users", scopeRoot)) // different scheme
}
