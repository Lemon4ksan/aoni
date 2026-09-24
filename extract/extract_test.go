// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package extract_test

import (
	"regexp"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	"github.com/lemon4ksan/aoni/extract"
)

func TestBetween(t *testing.T) {
	t.Parallel()

	src := []byte("prefix:hello world:suffix")

	res, err := extract.Between(src, "prefix:", ":suffix")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(res))

	resResult := extract.BetweenResult(src, "prefix:", ":suffix")
	require.True(t, resResult.IsSuccess())
	val, err := resResult.Unwrap()
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(val))

	resStr := extract.BetweenString(src, "prefix:", ":suffix")
	require.True(t, resStr.IsSuccess())
	strVal, err := resStr.Unwrap()
	require.NoError(t, err)
	assert.Equal(t, "hello world", strVal)

	resOpt := extract.BetweenOptional(src, "prefix:", ":suffix")
	require.True(t, resOpt.IsPresent())
	optVal, ok := resOpt.Value()
	assert.True(t, ok)
	assert.Equal(t, "hello world", optVal)

	_, err = extract.Between(src, "missing:", ":suffix")
	assert.ErrorIs(t, err, extract.ErrBetweenNotFound)

	assert.False(t, extract.BetweenOptional(src, "missing:", ":suffix").IsPresent())

	_, err = extract.Between(src, "prefix:", ":missing")
	assert.ErrorIs(t, err, extract.ErrBetweenNotFound)
}

func TestAttr(t *testing.T) {
	t.Parallel()

	src := []byte(`<div id="test-id" data-token="secret-123" class="main"></div>`)

	val, err := extract.Attr(src, "#test-id", "data-token")
	require.NoError(t, err)
	assert.Equal(t, "secret-123", string(val))

	attrRes := extract.AttrResult(src, "#test-id", "data-token")
	require.True(t, attrRes.IsSuccess())
	attrBytes, err := attrRes.Unwrap()
	require.NoError(t, err)
	assert.Equal(t, "secret-123", string(attrBytes))

	attrStr := extract.AttrString(src, "#test-id", "data-token")
	require.True(t, attrStr.IsSuccess())
	strVal, err := attrStr.Unwrap()
	require.NoError(t, err)
	assert.Equal(t, "secret-123", strVal)

	attrOpt := extract.AttrOptional(src, "#test-id", "data-token")
	require.True(t, attrOpt.IsPresent())
	valOpt, ok := attrOpt.Value()
	assert.True(t, ok)
	assert.Equal(t, "secret-123", valOpt)

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

	rxRes := extract.RegexResult(src, `SessionToken:\s*([0-9a-z-]+)`)
	require.True(t, rxRes.IsSuccess())
	rxBytes, err := rxRes.Unwrap()
	require.NoError(t, err)
	assert.Equal(t, "98765-abcd", string(rxBytes))

	rxStr := extract.RegexString(src, `SessionToken:\s*([0-9a-z-]+)`)
	require.True(t, rxStr.IsSuccess())
	strVal, err := rxStr.Unwrap()
	require.NoError(t, err)
	assert.Equal(t, "98765-abcd", strVal)

	rxOpt := extract.RegexOptional(src, `SessionToken:\s*([0-9a-z-]+)`)
	require.True(t, rxOpt.IsPresent())
	optVal, ok := rxOpt.Value()
	assert.True(t, ok)
	assert.Equal(t, "98765-abcd", optVal)

	_, err = extract.Regex(src, `NonMatching:\s*(\d+)`)
	assert.ErrorIs(t, err, extract.ErrRegexMismatch)
}

func TestBetweenAll(t *testing.T) {
	t.Parallel()

	src := []byte("<item>apple</item><item>banana</item><item>cherry</item>")

	var items []string
	for match := range extract.BetweenAll(src, "<item>", "</item>") {
		items = append(items, string(match))
	}
	require.Equal(t, 3, len(items))
	assert.Equal(t, "apple", items[0])
	assert.Equal(t, "banana", items[1])
	assert.Equal(t, "cherry", items[2])

	// Early exit
	count := 0
	for range extract.BetweenAll(src, "<item>", "</item>") {
		count++
		if count == 2 {
			break
		}
	}
	assert.Equal(t, 2, count)

	// Missing boundaries
	count = 0
	for range extract.BetweenAll(src, "<notfound>", "</item>") {
		count++
	}
	assert.Equal(t, 0, count)

	// Empty prefix and suffix
	count = 0
	for range extract.BetweenAll(src, "", "") {
		count++
	}
	assert.Equal(t, 0, count)
}

func TestBetweenAllString(t *testing.T) {
	t.Parallel()

	src := "tag:val1;tag:val2;tag:val3;"

	var items []string
	for match := range extract.BetweenAllString(src, "tag:", ";") {
		items = append(items, match)
	}
	require.Equal(t, 3, len(items))
	assert.Equal(t, "val1", items[0])
	assert.Equal(t, "val2", items[1])
	assert.Equal(t, "val3", items[2])

	// Early exit
	count := 0
	for range extract.BetweenAllString(src, "tag:", ";") {
		count++
		if count == 1 {
			break
		}
	}
	assert.Equal(t, 1, count)
}

func TestRegexAll(t *testing.T) {
	t.Parallel()

	src := []byte("item:10 item:20 item:30")
	rxWithGroup := regexp.MustCompile(`item:(\d+)`)
	rxWithoutGroup := regexp.MustCompile(`item:\d+`)

	var vals []string
	for m := range extract.RegexAll(src, rxWithGroup) {
		vals = append(vals, string(m))
	}
	require.Equal(t, 3, len(vals))
	assert.Equal(t, "10", vals[0])
	assert.Equal(t, "20", vals[1])
	assert.Equal(t, "30", vals[2])

	var fullVals []string
	for m := range extract.RegexAll(src, rxWithoutGroup) {
		fullVals = append(fullVals, string(m))
	}
	require.Equal(t, 3, len(fullVals))
	assert.Equal(t, "item:10", fullVals[0])

	// RegexAllSubmatch
	var submatches [][][]byte
	for sm := range extract.RegexAllSubmatch(src, rxWithGroup) {
		submatches = append(submatches, sm)
	}
	require.Equal(t, 3, len(submatches))
	assert.Equal(t, "item:10", string(submatches[0][0]))
	assert.Equal(t, "10", string(submatches[0][1]))

	// Nil regex
	for range extract.RegexAll(src, nil) {
		t.Fatal("expected no matches for nil regex")
	}
	for range extract.RegexAllSubmatch(src, nil) {
		t.Fatal("expected no matches for nil regex")
	}
}

func TestAttrsAll(t *testing.T) {
	t.Parallel()

	src := []byte(`<a href="https://example.com" data-id='1'>link</a><a href='https://foo.bar' data-id="2">link2</a>`)

	var hrefs []string
	for h := range extract.AttrsAll(src, "href") {
		hrefs = append(hrefs, string(h))
	}
	require.Equal(t, 2, len(hrefs))
	assert.Equal(t, "https://example.com", hrefs[0])
	assert.Equal(t, "https://foo.bar", hrefs[1])

	var ids []string
	for id := range extract.AttrsAll(src, "data-id") {
		ids = append(ids, string(id))
	}
	require.Equal(t, 2, len(ids))
	assert.Equal(t, "1", ids[0])
	assert.Equal(t, "2", ids[1])

	// Early exit
	count := 0
	for range extract.AttrsAll(src, "href") {
		count++
		break
	}
	assert.Equal(t, 1, count)

	// Empty attr
	for range extract.AttrsAll(src, "") {
		t.Fatal("expected no matches for empty attr")
	}
}

func BenchmarkBetweenAll_ZeroAlloc(b *testing.B) {
	src := []byte("<val>one</val><val>two</val><val>three</val><val>four</val><val>five</val>")
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		for item := range extract.BetweenAll(src, "<val>", "</val>") {
			if len(item) == 0 {
				b.Fatal("unexpected empty")
			}
		}
	}
}

func BenchmarkBetweenAllString_ZeroAlloc(b *testing.B) {
	src := "<val>one</val><val>two</val><val>three</val><val>four</val><val>five</val>"
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		for item := range extract.BetweenAllString(src, "<val>", "</val>") {
			if len(item) == 0 {
				b.Fatal("unexpected empty")
			}
		}
	}
}
