// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bytesconv

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestB2SAndS2B(t *testing.T) {
	t.Parallel()

	assert.Empty(t, B2S(nil))
	assert.Nil(t, S2B(""))

	str := "Hello, Aoni!"

	b := S2B(str)
	assert.True(t, bytes.Equal(b, []byte(str)))

	s := B2S(b)
	assert.Equal(t, str, s)
}

func TestLowercaseByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   byte
		want byte
	}{
		{'A', 'a'},
		{'Z', 'z'},
		{'a', 'a'},
		{'z', 'z'},
		{'0', '0'},
		{'-', '-'},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, LowercaseByte(tt.in))
	}
}

func TestEqualFoldASCII(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"Content-Type", "content-type", true},
		{"USER-AGENT", "user-agent", true},
		{"Hello", "World", false},
		{"Short", "LongerString", false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, EqualFoldASCII(tt.a, tt.b))
	}
}

func TestAppendToLower(t *testing.T) {
	t.Parallel()

	assert.Empty(t, AppendToLower(nil, nil))

	dst := []byte("Header: ")
	src := []byte("X-Aoni-Test")
	got := AppendToLower(dst, src)

	assert.Equal(t, "Header: x-aoni-test", string(got))
}

func TestTrimQuotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{`"hello"`, "hello"},
		{`"a"`, "a"},
		{`""`, ""},
		{`noquotes`, "noquotes"},
		{`"unmatched`, `"unmatched`},
	}

	for _, tt := range tests {
		got := TrimQuotes([]byte(tt.in))
		assert.Equal(t, tt.want, string(got))
	}
}

func TestContainsFoldASCII(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src    string
		target string
		want   bool
	}{
		{"gzip, deflate", "gzip", true},
		{"Bz gZip", "gzip", true},
		{"GZIP", "gzip", true},
		{"gZip", "gzip", true},
		{"BR", "br", true},
		{"zStD, gzip", "zstd", true},
		{"gzip", "br", false},
		{"", "gzip", false},
		{"gzip", "", true},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, ContainsFoldASCII([]byte(tt.src), tt.target))
	}
}

func TestParseUintFast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		wantVal int64
		wantOK  bool
	}{
		{"200", 200, true},
		{"0", 0, true},
		{"1234567890", 1234567890, true},
		{"", 0, false},
		{"abc", 0, false},
		{"12a34", 0, false},
	}

	for _, tt := range tests {
		val, ok := ParseUintFast([]byte(tt.in))
		assert.Equal(t, tt.wantOK, ok)

		if ok {
			assert.Equal(t, tt.wantVal, val)
		}
	}
}

func TestFastHash64(t *testing.T) {
	t.Parallel()

	h1 := FastHash64([]byte("hello world"))
	h2 := FastHash64([]byte("hello world"))
	h3 := FastHash64([]byte("different data"))

	assert.Equal(t, h1, h2)
	assert.NotEqual(t, h1, h3)
	assert.NotZero(t, FastHash64(nil))
}
