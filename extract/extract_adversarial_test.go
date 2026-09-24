// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package extract_test

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	"github.com/lemon4ksan/aoni/extract"
)

func TestBetweenAll_Adversarial_EarlyExit(t *testing.T) {
	t.Parallel()

	src := []byte("<item>1</item><item>2</item><item>3</item><item>4</item><item>5</item>")

	t.Run("exit after 0 (first yield returns false)", func(t *testing.T) {
		count := 0
		extract.BetweenAll(src, "<item>", "</item>")(func(b []byte) bool {
			count++
			return false
		})
		assert.Equal(t, 1, count)
	})

	t.Run("exit after 1 iteration", func(t *testing.T) {
		count := 0
		for val := range extract.BetweenAll(src, "<item>", "</item>") {
			count++
			assert.Equal(t, []byte("1"), val)
			if count == 1 {
				break
			}
		}
		assert.Equal(t, 1, count)
	})

	t.Run("exit mid sequence", func(t *testing.T) {
		count := 0
		for range extract.BetweenAll(src, "<item>", "</item>") {
			count++
			if count == 3 {
				break
			}
		}
		assert.Equal(t, 3, count)
	})
}

func TestBetweenAll_Adversarial_Boundaries(t *testing.T) {
	t.Parallel()

	t.Run("empty src or nil", func(t *testing.T) {
		count := 0
		for range extract.BetweenAll(nil, "[", "]") {
			count++
		}
		assert.Equal(t, 0, count)

		count = 0
		for range extract.BetweenAll([]byte{}, "[", "]") {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("both prefix and suffix empty", func(t *testing.T) {
		count := 0
		for range extract.BetweenAll([]byte("hello"), "", "") {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("empty prefix with suffix", func(t *testing.T) {
		src := []byte("apple,banana,orange,")
		var parts [][]byte
		for p := range extract.BetweenAll(src, "", ",") {
			parts = append(parts, p)
		}
		require.Equal(t, 3, len(parts))
		assert.Equal(t, []byte("apple"), parts[0])
		assert.Equal(t, []byte("banana"), parts[1])
		assert.Equal(t, []byte("orange"), parts[2])
	})

	t.Run("prefix with empty suffix", func(t *testing.T) {
		src := []byte("prefix:rest of data")
		var parts [][]byte
		for p := range extract.BetweenAll(src, "prefix:", "") {
			parts = append(parts, p)
		}
		require.Equal(t, 1, len(parts))
		assert.Equal(t, []byte("rest of data"), parts[0])
	})

	t.Run("missing suffix", func(t *testing.T) {
		src := []byte("[unclosed bracket string")
		count := 0
		for range extract.BetweenAll(src, "[", "]") {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("repetitive adjacent delimiters", func(t *testing.T) {
		src := []byte("[][][]")
		var matches [][]byte
		for m := range extract.BetweenAll(src, "[", "]") {
			matches = append(matches, m)
		}
		require.Equal(t, 3, len(matches))
		for _, m := range matches {
			assert.Equal(t, 0, len(m))
		}
	})

	t.Run("nested delimiters", func(t *testing.T) {
		src := []byte("[[nested]]")
		var matches [][]byte
		for m := range extract.BetweenAll(src, "[", "]") {
			matches = append(matches, m)
		}
		require.Equal(t, 1, len(matches))
		assert.Equal(t, []byte("[nested"), matches[0])
	})
}

func TestBetweenAllString_Adversarial(t *testing.T) {
	t.Parallel()

	s := "{{one}}{{two}}{{three}}"

	t.Run("early exit", func(t *testing.T) {
		count := 0
		extract.BetweenAllString(s, "{{", "}}")(func(str string) bool {
			count++
			return false
		})
		assert.Equal(t, 1, count)
	})

	t.Run("empty boundaries", func(t *testing.T) {
		count := 0
		for range extract.BetweenAllString("test", "", "") {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("missing suffix", func(t *testing.T) {
		count := 0
		for range extract.BetweenAllString("prefix unclosed", "prefix ", "!") {
			count++
		}
		assert.Equal(t, 0, count)
	})
}

func TestAttrsAll_Adversarial(t *testing.T) {
	t.Parallel()

	t.Run("early exit after 0 (first yield returns false)", func(t *testing.T) {
		src := []byte(`<a href="url1"></a><a href="url2"></a>`)
		count := 0
		extract.AttrsAll(src, "href")(func(val []byte) bool {
			count++
			return false
		})
		assert.Equal(t, 1, count)
	})

	t.Run("early exit mid-sequence", func(t *testing.T) {
		src := []byte(`<span class="c1"></span><span class="c2"></span><span class="c3"></span>`)
		count := 0
		for range extract.AttrsAll(src, "class") {
			count++
			if count == 2 {
				break
			}
		}
		assert.Equal(t, 2, count)
	})

	t.Run("empty attrName or empty src", func(t *testing.T) {
		count := 0
		for range extract.AttrsAll([]byte(`<a href="x">`), "") {
			count++
		}
		assert.Equal(t, 0, count)

		count = 0
		for range extract.AttrsAll(nil, "href") {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("mixed quotes and unclosed quote", func(t *testing.T) {
		src := []byte(`<div data="first" data='second' data="unclosed`)
		var vals [][]byte
		for v := range extract.AttrsAll(src, "data") {
			vals = append(vals, v)
		}
		require.Equal(t, 2, len(vals))
		assert.Equal(t, []byte("first"), vals[0])
		assert.Equal(t, []byte("second"), vals[1])
	})

	t.Run("empty attribute value", func(t *testing.T) {
		src := []byte(`<div class="" class=''>`)
		var vals [][]byte
		for v := range extract.AttrsAll(src, "class") {
			vals = append(vals, v)
		}
		require.Equal(t, 2, len(vals))
		assert.Equal(t, 0, len(vals[0]))
		assert.Equal(t, 0, len(vals[1]))
	})
}

func TestRegexAll_Adversarial(t *testing.T) {
	t.Parallel()

	src := []byte("user:101; user:102; user:103; admin:999;")

	t.Run("nil regex", func(t *testing.T) {
		count := 0
		for range extract.RegexAll(src, nil) {
			count++
		}
		assert.Equal(t, 0, count)

		for range extract.RegexAllSubmatch(src, nil) {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("RegexAll early exit", func(t *testing.T) {
		rx := regexp.MustCompile(`user:(\d+)`)
		count := 0
		extract.RegexAll(src, rx)(func(m []byte) bool {
			count++
			return false
		})
		assert.Equal(t, 1, count)
	})

	t.Run("RegexAllSubmatch early exit", func(t *testing.T) {
		rx := regexp.MustCompile(`user:(\d+)`)
		count := 0
		extract.RegexAllSubmatch(src, rx)(func(sm [][]byte) bool {
			count++
			return false
		})
		assert.Equal(t, 1, count)
	})

	t.Run("no match regex", func(t *testing.T) {
		rx := regexp.MustCompile(`nomatch:(\d+)`)
		count := 0
		for range extract.RegexAll(src, rx) {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("regex without capture group yields full match", func(t *testing.T) {
		rx := regexp.MustCompile(`admin:\d+`)
		var matches [][]byte
		for m := range extract.RegexAll(src, rx) {
			matches = append(matches, m)
		}
		require.Equal(t, 1, len(matches))
		assert.Equal(t, []byte("admin:999"), matches[0])
	})
}

func TestExtract_Adversarial_ExtremeInputs(t *testing.T) {
	t.Parallel()

	t.Run("massive buffer 5000 occurrences", func(t *testing.T) {
		n := 5000
		var sb strings.Builder
		for i := range n {
			fmt.Fprintf(&sb, "<tag>%d</tag>", i)
		}
		data := []byte(sb.String())

		count := 0
		for val := range extract.BetweenAll(data, "<tag>", "</tag>") {
			expected := fmt.Sprintf("%d", count)
			assert.Equal(t, []byte(expected), val)
			count++
		}
		assert.Equal(t, n, count)
	})
}

func TestExtract_Adversarial_Concurrency(t *testing.T) {
	t.Parallel()

	data := []byte(
		`<div id="main" class="container"><span class="highlight">1</span><span class="highlight">2</span></div>`,
	)
	rx := regexp.MustCompile(`class="([^"]+)"`)

	var wg sync.WaitGroup
	goroutines := 30

	for range goroutines {
		wg.Add(3)

		go func() {
			defer wg.Done()
			count := 0
			for range extract.BetweenAll(data, `<span class="highlight">`, `</span>`) {
				count++
			}
			assert.Equal(t, 2, count)
		}()

		go func() {
			defer wg.Done()
			count := 0
			for range extract.AttrsAll(data, "class") {
				count++
			}
			assert.Equal(t, 3, count)
		}()

		go func() {
			defer wg.Done()
			count := 0
			for range extract.RegexAll(data, rx) {
				count++
			}
			assert.Equal(t, 3, count)
		}()
	}

	wg.Wait()
}

func TestExtract_Adversarial_ZeroAlloc(t *testing.T) {
	data := []byte(`<item>alpha</item><item>beta</item><item>gamma</item>`)
	dataStr := `<item>alpha</item><item>beta</item><item>gamma</item>`
	htmlData := []byte(`<a href="u1"></a><a href="u2"></a><a href="u3"></a>`)

	allocsBetween := testing.AllocsPerRun(100, func() {
		for m := range extract.BetweenAll(data, "<item>", "</item>") {
			if len(m) == 0 {
				t.Fatal("empty")
			}
		}
	})
	assert.Equal(t, float64(0), allocsBetween, "BetweenAll must be 0 allocs/op")

	allocsBetweenStr := testing.AllocsPerRun(100, func() {
		for m := range extract.BetweenAllString(dataStr, "<item>", "</item>") {
			if len(m) == 0 {
				t.Fatal("empty")
			}
		}
	})
	assert.Equal(t, float64(0), allocsBetweenStr, "BetweenAllString must be 0 allocs/op")

	allocsAttrs := testing.AllocsPerRun(100, func() {
		for m := range extract.AttrsAll(htmlData, "href") {
			if len(m) == 0 {
				t.Fatal("empty")
			}
		}
	})
	assert.Equal(t, float64(0), allocsAttrs, "AttrsAll must be 0 allocs/op")
}
