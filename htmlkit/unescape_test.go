// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package htmlkit_test

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/lemon4ksan/foundation/testing/assert"

	"github.com/lemon4ksan/aoni/htmlkit"
)

func TestUnescape_AllNamedEntities(t *testing.T) {
	t.Parallel()

	namedEntities := []struct {
		entity   string
		expected string
	}{
		{"&quot;", `"`},
		{"&amp;", `&`},
		{"&apos;", `'`},
		{"&lt;", `<`},
		{"&gt;", `>`},
		{"&nbsp;", "\u00A0"},
		{"&copy;", "\u00A9"},
		{"&reg;", "\u00AE"},
		{"&trade;", "\u2122"},
		{"&euro;", "\u20AC"},
		{"&cent;", "\u00A2"},
		{"&pound;", "\u00A3"},
		{"&yen;", "\u00A5"},
		{"&sect;", "\u00A7"},
		{"&deg;", "\u00B0"},
		{"&plusmn;", "\u00B1"},
		{"&para;", "\u00B6"},
		{"&middot;", "\u00B7"},
		{"&times;", "\u00D7"},
		{"&divide;", "\u00F7"},
		{"&hellip;", "\u2026"},
		{"&ndash;", "\u2013"},
		{"&mdash;", "\u2014"},
		{"&lsquo;", "\u2018"},
		{"&rsquo;", "\u2019"},
		{"&sbquo;", "\u201A"},
		{"&ldquo;", "\u201C"},
		{"&rdquo;", "\u201D"},
		{"&bdquo;", "\u201E"},
		{"&dagger;", "\u2020"},
		{"&Dagger;", "\u2021"},
		{"&permil;", "\u2030"},
		{"&lsaquo;", "\u2039"},
		{"&rsaquo;", "\u203A"},
		{"&bull;", "\u2022"},
		{"&prime;", "\u2032"},
		{"&Prime;", "\u2033"},
		{"&oline;", "\u203E"},
		{"&frasl;", "\u2044"},
		{"&weierp;", "\u2118"},
		{"&image;", "\u2111"},
		{"&real;", "\u211C"},
		{"&alefsym;", "\u2135"},
		{"&larr;", "\u2190"},
		{"&uarr;", "\u2191"},
		{"&rarr;", "\u2192"},
		{"&darr;", "\u2193"},
		{"&harr;", "\u2194"},
		{"&crarr;", "\u21B5"},
		{"&lArr;", "\u21D0"},
		{"&uArr;", "\u21D1"},
		{"&rArr;", "\u21D2"},
		{"&dArr;", "\u21D3"},
		{"&hArr;", "\u21D4"},
		{"&forall;", "\u2200"},
		{"&part;", "\u2202"},
		{"&exist;", "\u2203"},
		{"&empty;", "\u2205"},
		{"&nabla;", "\u2207"},
		{"&isin;", "\u2208"},
		{"&notin;", "\u2209"},
		{"&ni;", "\u220B"},
		{"&prod;", "\u220F"},
		{"&sum;", "\u2211"},
		{"&minus;", "\u2212"},
		{"&lowast;", "\u2217"},
		{"&radic;", "\u221A"},
		{"&prop;", "\u221D"},
		{"&infin;", "\u221E"},
		{"&ang;", "\u2220"},
		{"&and;", "\u2227"},
		{"&or;", "\u2228"},
		{"&cap;", "\u2229"},
		{"&cup;", "\u222A"},
		{"&int;", "\u222B"},
		{"&there4;", "\u2234"},
		{"&sim;", "\u223C"},
		{"&cong;", "\u2245"},
		{"&asymp;", "\u2248"},
		{"&ne;", "\u2260"},
		{"&equiv;", "\u2261"},
		{"&le;", "\u2264"},
		{"&ge;", "\u2265"},
		{"&sub;", "\u2282"},
		{"&sup;", "\u2283"},
		{"&nsub;", "\u2284"},
		{"&sube;", "\u2286"},
		{"&supe;", "\u2287"},
		{"&oplus;", "\u2295"},
		{"&otimes;", "\u2297"},
		{"&perp;", "\u22A5"},
		{"&sdot;", "\u22C5"},
		{"&lceil;", "\u2308"},
		{"&rceil;", "\u2309"},
		{"&lfloor;", "\u230A"},
		{"&rfloor;", "\u230B"},
		{"&lang;", "\u2329"},
		{"&rang;", "\u232A"},
		{"&loz;", "\u25CA"},
		{"&spades;", "\u2660"},
		{"&clubs;", "\u2663"},
		{"&hearts;", "\u2665"},
		{"&diams;", "\u2666"},
	}

	for _, tt := range namedEntities {
		t.Run(tt.entity, func(t *testing.T) {
			input := fmt.Sprintf("prefix %s suffix", tt.entity)
			expected := fmt.Sprintf("prefix %s suffix", tt.expected)
			assert.Equal(t, []byte(expected), htmlkit.Unescape([]byte(input)))
		})
	}
}

func TestUnescape_NumericAndEdgeCases(t *testing.T) {
	t.Parallel()

	// 1. Plain text with no ampersand (fast path)
	assert.Equal(t, []byte("plain text"), htmlkit.Unescape([]byte("plain text")))
	assert.Equal(t, []byte(""), htmlkit.Unescape([]byte("")))

	// 2. Numeric Decimal
	assert.Equal(t, []byte("A"), htmlkit.Unescape([]byte("&#65;")))
	assert.Equal(t, []byte("0"), htmlkit.Unescape([]byte("&#48;")))
	assert.Equal(t, []byte(string(rune(utf8.MaxRune))), htmlkit.Unescape([]byte(fmt.Sprintf("&#%d;", utf8.MaxRune))))

	// Decimal overflow / surrogate / invalid
	assert.Equal(t, []byte("&#99999999;"), htmlkit.Unescape([]byte("&#99999999;")))       // len > 7
	assert.Equal(t, []byte("&#2000000;"), htmlkit.Unescape([]byte("&#2000000;")))         // val > MaxRune returns false
	assert.Equal(t, []byte(string(utf8.RuneError)), htmlkit.Unescape([]byte("&#55296;"))) // surrogate 0xD800
	assert.Equal(t, []byte("&#12a;"), htmlkit.Unescape([]byte("&#12a;")))                 // non-digit

	// 3. Numeric Hexadecimal
	assert.Equal(t, []byte("😀"), htmlkit.Unescape([]byte("&#x1F600;")))
	assert.Equal(t, []byte("A"), htmlkit.Unescape([]byte("&#X41;")))
	assert.Equal(t, []byte("0"), htmlkit.Unescape([]byte("&#x30;")))
	assert.Equal(t, []byte("f"), htmlkit.Unescape([]byte("&#x66;")))
	assert.Equal(t, []byte("F"), htmlkit.Unescape([]byte("&#x46;")))

	// Hex invalid / overflow
	assert.Equal(t, []byte("&#x;"), htmlkit.Unescape([]byte("&#x;")))                       // empty hex
	assert.Equal(t, []byte("&#x1234567;"), htmlkit.Unescape([]byte("&#x1234567;")))         // len > 6
	assert.Equal(t, []byte("&#x1G;"), htmlkit.Unescape([]byte("&#x1G;")))                   // invalid hex digit
	assert.Equal(t, []byte(string(utf8.RuneError)), htmlkit.Unescape([]byte("&#x200000;"))) // val > MaxRune

	// 4. Broken / malformed entities
	assert.Equal(t, []byte("&"), htmlkit.Unescape([]byte("&")))
	assert.Equal(t, []byte("&;"), htmlkit.Unescape([]byte("&;")))
	assert.Equal(t, []byte("&#;"), htmlkit.Unescape([]byte("&#;")))

	longEntity := []byte("&verylongentitynameexceedinglimit;")
	assert.Equal(t, longEntity, htmlkit.Unescape(longEntity))
	assert.Equal(t, []byte("&unknownentity;"), htmlkit.Unescape([]byte("&unknownentity;")))
	assert.Equal(t, []byte("start & end"), htmlkit.Unescape([]byte("start & end")))
}
