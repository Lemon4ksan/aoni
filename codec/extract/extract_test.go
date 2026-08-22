// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package extract_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/codec/extract"
)

func TestBetween(t *testing.T) {
	t.Parallel()

	src := []byte("prefix:hello world:suffix")

	res, err := extract.Between(src, "prefix:", ":suffix")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(res))

	resResult := generic.FromResult(extract.Between(src, "prefix:", ":suffix"))
	require.True(t, resResult.IsSuccess())
	assert.Equal(t, "hello world", string(resResult.MustValue()))

	_, err = extract.Between(src, "missing:", ":suffix")
	assert.ErrorIs(t, err, extract.ErrBetweenNotFound)

	_, err = extract.Between(src, "prefix:", ":missing")
	assert.ErrorIs(t, err, extract.ErrBetweenNotFound)
}

func TestAttr(t *testing.T) {
	t.Parallel()

	src := []byte(`<div id="test-id" data-token="secret-123" class="main"></div>`)

	val, err := extract.Attr(src, "#test-id", "data-token")
	require.NoError(t, err)
	assert.Equal(t, "secret-123", string(val))

	attrRes := generic.FromResult(extract.Attr(src, "#test-id", "data-token"))
	require.True(t, attrRes.IsSuccess())
	assert.Equal(t, "secret-123", string(attrRes.MustValue()))

	_, err = extract.Attr(src, "#missing-id", "data-token")
	assert.ErrorIs(t, err, extract.ErrElementNotFound)

	_, err = extract.Attr(src, "#test-id", "missing-attr")
	assert.ErrorIs(t, err, extract.ErrAttrNotFound)
}

func TestRegex(t *testing.T) {
	t.Parallel()

	src := []byte("SessionToken: 98765-abcd")

	val, err := extract.Regex(src, `SessionToken:\s*([0-9a-z-]+)`)
	require.NoError(t, err)
	assert.Equal(t, "98765-abcd", string(val))

	rxRes := generic.FromResult(extract.Regex(src, `SessionToken:\s*([0-9a-z-]+)`))
	require.True(t, rxRes.IsSuccess())
	assert.Equal(t, "98765-abcd", string(rxRes.MustValue()))

	_, err = extract.Regex(src, `NonMatching:\s*(\d+)`)
	assert.ErrorIs(t, err, extract.ErrRegexMismatch)
}

func TestHTMLUnescape(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no entities (zero alloc)",
			input:    "plain ascii string without entities",
			expected: "plain ascii string without entities",
		},
		{
			name:     "standard XML entities",
			input:    "&lt;div class=&quot;main&quot;&gt;&amp;apos;&lt;/div&gt;",
			expected: `<div class="main">&apos;</div>`,
		},
		{
			name:     "named typographic entities",
			input:    "Price: &euro;100 &copy; 2026 &ndash; &mdash; &hellip;",
			expected: "Price: €100 © 2026 – — …",
		},
		{
			name:     "decimal numeric entities",
			input:    "&#60;&#62;&#38;&#34;",
			expected: `<>&"`,
		},
		{
			name:     "hex numeric entities",
			input:    "&#x3C;&#x3E;&#x26;&#x22; emoji: &#x1F600;",
			expected: `<>&" emoji: 😀`,
		},
		{
			name:     "unclosed and invalid entities",
			input:    "foo & bar &invalidEntityNameHere; baz &#x &#999999999;",
			expected: "foo & bar &invalidEntityNameHere; baz &#x &#999999999;",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := extract.HTMLUnescape([]byte(tc.input))
			assert.Equal(t, tc.expected, string(res))
		})
	}
}

func BenchmarkHTMLUnescape_NoEntities(b *testing.B) {
	src := []byte("plain ascii text without any html entities to demonstrate zero allocations")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = extract.HTMLUnescape(src)
	}
}

func BenchmarkHTMLUnescape_WithEntities(b *testing.B) {
	src := []byte("&lt;div class=&quot;test&quot;&gt;&amp;copy; 2026 &#x1F600;&lt;/div&gt;")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = extract.HTMLUnescape(src)
	}
}
