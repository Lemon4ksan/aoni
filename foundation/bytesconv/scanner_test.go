// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bytesconv_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/bytesconv"
)

func TestTrimSpaceASCII(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", bytesconv.TrimSpaceASCII("  hello  \r\n\t"))
	assert.Equal(t, "", bytesconv.TrimSpaceASCII("   \t\r\n "))
	assert.Equal(t, "a", bytesconv.TrimSpaceASCII("a"))
	assert.Equal(t, "", bytesconv.TrimSpaceASCII(""))

	b := bytesconv.TrimSpaceASCIIBytes([]byte("  data \n"))
	assert.Equal(t, "data", string(b))
}

func TestCutByte(t *testing.T) {
	t.Parallel()

	before, after, found := bytesconv.CutByte("key=value", '=')
	assert.True(t, found)
	assert.Equal(t, "key", before)
	assert.Equal(t, "value", after)

	b, a, f := bytesconv.CutByte("single", '=')
	assert.False(t, f)
	assert.Equal(t, "single", b)
	assert.Empty(t, a)

	bb, ba, bf := bytesconv.CutByteBytes([]byte("key:value"), ':')
	assert.True(t, bf)
	assert.Equal(t, "key", string(bb))
	assert.Equal(t, "value", string(ba))
}

func TestScanTokens(t *testing.T) {
	t.Parallel()

	raw := "gzip, deflate, br, \"quoted,token,intact\", zstd,  "

	var tokens []string
	for tok := range bytesconv.ScanTokens(raw, ',') {
		tokens = append(tokens, tok)
	}

	expected := []string{
		"gzip",
		"deflate",
		"br",
		"\"quoted,token,intact\"",
		"zstd",
	}
	assert.Equal(t, expected, tokens)

	// Early exit test
	var firstTwo []string
	for tok := range bytesconv.ScanTokens(raw, ',') {
		firstTwo = append(firstTwo, tok)
		if len(firstTwo) == 2 {
			break
		}
	}

	assert.Equal(t, []string{"gzip", "deflate"}, firstTwo)
}

func TestScanTokensBytes(t *testing.T) {
	t.Parallel()

	raw := []byte("a; b; \"c;d\"; e")

	var tokens []string
	for tok := range bytesconv.ScanTokensBytes(raw, ';') {
		tokens = append(tokens, string(tok))
	}

	assert.Equal(t, []string{"a", "b", "\"c;d\"", "e"}, tokens)
}

func TestScanPairs(t *testing.T) {
	t.Parallel()

	cookieHeader := "session_id=xyz123; Path=/; Secure; HttpOnly; SameSite=Lax; custom=\"with=equals;and;semicolon\""
	pairs := make(map[string]string)

	for k, v := range bytesconv.ScanPairs(cookieHeader, ';', '=') {
		pairs[k] = v
	}

	assert.Equal(t, "xyz123", pairs["session_id"])
	assert.Equal(t, "/", pairs["Path"])
	assert.Equal(t, "", pairs["Secure"])
	assert.Equal(t, "", pairs["HttpOnly"])
	assert.Equal(t, "Lax", pairs["SameSite"])
	assert.Equal(t, "with=equals;and;semicolon", pairs["custom"])
}

func TestScanPairsBytes(t *testing.T) {
	t.Parallel()

	query := []byte("q=golang&page=1&sort=desc&empty=&flag")
	pairs := make(map[string]string)

	for k, v := range bytesconv.ScanPairsBytes(query, '&', '=') {
		pairs[string(k)] = string(v)
	}

	assert.Equal(t, "golang", pairs["q"])
	assert.Equal(t, "1", pairs["page"])
	assert.Equal(t, "desc", pairs["sort"])
	assert.Equal(t, "", pairs["empty"])
	assert.Equal(t, "", pairs["flag"])
}

func BenchmarkScanTokens_ZeroAlloc(t *testing.B) {
	raw := "gzip, deflate, br, zstd, identity"

	t.ReportAllocs()
	t.ResetTimer()

	for i := 0; i < t.N; i++ {
		count := 0
		for tok := range bytesconv.ScanTokens(raw, ',') {
			if len(tok) > 0 {
				count++
			}
		}

		if count != 5 {
			t.Fatalf("unexpected count: %d", count)
		}
	}
}

func BenchmarkScanPairs_ZeroAlloc(t *testing.B) {
	raw := "session=abc; Path=/api; Secure; HttpOnly; Max-Age=3600"

	t.ReportAllocs()
	t.ResetTimer()

	for i := 0; i < t.N; i++ {
		count := 0
		for k, v := range bytesconv.ScanPairs(raw, ';', '=') {
			if len(k) > 0 || len(v) > 0 {
				count++
			}
		}

		if count != 5 {
			t.Fatalf("unexpected count: %d", count)
		}
	}
}
