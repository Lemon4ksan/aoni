// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package htmlkit implements zero-allocation HTML entity decoding and unescaping conforming to HTML5 and XML specifications.
package htmlkit

import (
	"bytes"
	"unicode/utf8"
)

// Unescape converts HTML entities within src into their unescaped UTF-8 byte representation.
// If src contains no '&' character, it returns src unchanged with zero allocations.
func Unescape(src []byte) []byte {
	if bytes.IndexByte(src, '&') == -1 {
		return src
	}

	dst := make([]byte, 0, len(src))

	return AppendUnescape(dst, src)
}

// AppendUnescape parses HTML entities in src and appends the decoded UTF-8 bytes into dst.
// Performs zero string conversions and zero intermediate allocations.
func AppendUnescape(dst, src []byte) []byte {
	for len(src) > 0 {
		idx := bytes.IndexByte(src, '&')
		if idx == -1 {
			return append(dst, src...)
		}

		dst = append(dst, src[:idx]...)
		src = src[idx:]

		// Find ending semicolon ';'
		semi := bytes.IndexByte(src, ';')
		if semi == -1 || semi > 12 { // HTML entities do not exceed 12 bytes
			dst = append(dst, '&')
			src = src[1:]
			continue
		}

		entity := src[1:semi]
		if r, ok := parseHTMLEntity(entity); ok {
			dst = utf8.AppendRune(dst, r)
			src = src[semi+1:]
		} else {
			dst = append(dst, '&')
			src = src[1:]
		}
	}

	return dst
}

func parseHTMLEntity(entity []byte) (rune, bool) {
	if len(entity) == 0 {
		return 0, false
	}

	if entity[0] == '#' {
		return parseNumericEntity(entity[1:])
	}

	return parseNamedEntity(entity)
}

func parseNumericEntity(num []byte) (rune, bool) {
	if len(num) == 0 {
		return 0, false
	}

	var val uint32

	if num[0] == 'x' || num[0] == 'X' {
		// Hexadecimal &#x1F600;
		hexDigits := num[1:]
		if len(hexDigits) == 0 || len(hexDigits) > 6 {
			return 0, false
		}

		for _, c := range hexDigits {
			val <<= 4
			switch {
			case c >= '0' && c <= '9':
				val |= uint32(c - '0')
			case c >= 'a' && c <= 'f':
				val |= uint32(c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				val |= uint32(c - 'A' + 10)
			default:
				return 0, false
			}
		}
	} else {
		// Decimal &#1234;
		if len(num) > 7 {
			return 0, false
		}

		for _, c := range num {
			if c < '0' || c > '9' {
				return 0, false
			}

			val = val*10 + uint32(c-'0')
			if val > utf8.MaxRune {
				return 0, false
			}
		}
	}

	if val > utf8.MaxRune || (val >= 0xD800 && val <= 0xDFFF) {
		return utf8.RuneError, true
	}

	return rune(val), true
}

func parseNamedEntity(name []byte) (rune, bool) {
	switch string(name) {
	// Standard XML/HTML5 base entities (fast match)
	case "quot":
		return '"', true
	case "amp":
		return '&', true
	case "apos":
		return '\'', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "nbsp":
		return '\u00A0', true

	// Typography & Common Symbols
	case "copy":
		return '\u00A9', true
	case "reg":
		return '\u00AE', true
	case "trade":
		return '\u2122', true
	case "euro":
		return '\u20AC', true
	case "cent":
		return '\u00A2', true
	case "pound":
		return '\u00A3', true
	case "yen":
		return '\u00A5', true
	case "sect":
		return '\u00A7', true
	case "deg":
		return '\u00B0', true
	case "plusmn":
		return '\u00B1', true
	case "para":
		return '\u00B6', true
	case "middot":
		return '\u00B7', true
	case "times":
		return '\u00D7', true
	case "divide":
		return '\u00F7', true
	case "hellip":
		return '\u2026', true
	case "ndash":
		return '\u2013', true
	case "mdash":
		return '\u2014', true
	case "lsquo":
		return '\u2018', true
	case "rsquo":
		return '\u2019', true
	case "sbquo":
		return '\u201A', true
	case "ldquo":
		return '\u201C', true
	case "rdquo":
		return '\u201D', true
	case "bdquo":
		return '\u201E', true
	case "dagger":
		return '\u2020', true
	case "Dagger":
		return '\u2021', true
	case "permil":
		return '\u2030', true
	case "lsaquo":
		return '\u2039', true
	case "rsaquo":
		return '\u203A', true
	case "bull":
		return '\u2022', true
	case "prime":
		return '\u2032', true
	case "Prime":
		return '\u2033', true
	case "oline":
		return '\u203E', true
	case "frasl":
		return '\u2044', true
	case "weierp":
		return '\u2118', true
	case "image":
		return '\u2111', true
	case "real":
		return '\u211C', true
	case "alefsym":
		return '\u2135', true
	case "larr":
		return '\u2190', true
	case "uarr":
		return '\u2191', true
	case "rarr":
		return '\u2192', true
	case "darr":
		return '\u2193', true
	case "harr":
		return '\u2194', true
	case "crarr":
		return '\u21B5', true
	case "lArr":
		return '\u21D0', true
	case "uArr":
		return '\u21D1', true
	case "rArr":
		return '\u21D2', true
	case "dArr":
		return '\u21D3', true
	case "hArr":
		return '\u21D4', true
	case "forall":
		return '\u2200', true
	case "part":
		return '\u2202', true
	case "exist":
		return '\u2203', true
	case "empty":
		return '\u2205', true
	case "nabla":
		return '\u2207', true
	case "isin":
		return '\u2208', true
	case "notin":
		return '\u2209', true
	case "ni":
		return '\u220B', true
	case "prod":
		return '\u220F', true
	case "sum":
		return '\u2211', true
	case "minus":
		return '\u2212', true
	case "lowast":
		return '\u2217', true
	case "radic":
		return '\u221A', true
	case "prop":
		return '\u221D', true
	case "infin":
		return '\u221E', true
	case "ang":
		return '\u2220', true
	case "and":
		return '\u2227', true
	case "or":
		return '\u2228', true
	case "cap":
		return '\u2229', true
	case "cup":
		return '\u222A', true
	case "int":
		return '\u222B', true
	case "there4":
		return '\u2234', true
	case "sim":
		return '\u223C', true
	case "cong":
		return '\u2245', true
	case "asymp":
		return '\u2248', true
	case "ne":
		return '\u2260', true
	case "equiv":
		return '\u2261', true
	case "le":
		return '\u2264', true
	case "ge":
		return '\u2265', true
	case "sub":
		return '\u2282', true
	case "sup":
		return '\u2283', true
	case "nsub":
		return '\u2284', true
	case "sube":
		return '\u2286', true
	case "supe":
		return '\u2287', true
	case "oplus":
		return '\u2295', true
	case "otimes":
		return '\u2297', true
	case "perp":
		return '\u22A5', true
	case "sdot":
		return '\u22C5', true
	case "lceil":
		return '\u2308', true
	case "rceil":
		return '\u2309', true
	case "lfloor":
		return '\u230A', true
	case "rfloor":
		return '\u230B', true
	case "lang":
		return '\u2329', true
	case "rang":
		return '\u232A', true
	case "loz":
		return '\u25CA', true
	case "spades":
		return '\u2660', true
	case "clubs":
		return '\u2663', true
	case "hearts":
		return '\u2665', true
	case "diams":
		return '\u2666', true
	default:
		return 0, false
	}
}
