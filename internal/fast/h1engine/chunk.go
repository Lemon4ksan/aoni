// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"bufio"
)

// ParseHexUint parses a hex-encoded uint from src.
// Returns the parsed integer, number of bytes consumed, and error if malformed.
func ParseHexUint(src []byte) (int, int, error) {
	if hasVectorChunk {
		return vectorParseHexUint(src)
	}

	return parseHexUintFallback(src)
}

// ReadBodyChunked decodes an HTTP/1.1 chunked stream from r into dst (RFC 9112 §7.1).
func ReadBodyChunked(r *bufio.Reader, maxBodySize int, dst []byte) ([]byte, error) {
	return readBodyChunked(r, maxBodySize, dst)
}

// FormatHexUint writes the hex representation of val into buf.
// Returns the number of bytes written.
func FormatHexUint(buf *[16]byte, val int) int {
	if hasVectorChunk {
		return vectorFormatHexUint(buf, val)
	}

	return formatHexUintFallback(buf, val)
}

// FormatChunkHeader writes the hex chunk header with \r\n trailer into buf.
// Returns the number of bytes written.
func FormatChunkHeader(buf *[24]byte, val int) int {
	if hasVectorChunk {
		return vectorFormatChunkHeader(buf, val)
	}

	n := formatHexUintFallback((*[16]byte)(buf[:16]), val)
	buf[n] = '\r'
	buf[n+1] = '\n'
	return n + 2
}

func parseHexUintFallback(src []byte) (int, int, error) {
	if len(src) == 0 {
		return 0, 0, errEmptyHexNum
	}

	var n, i int
	for i = 0; i < len(src); i++ {
		c := src[i]
		k := int(hex2intTable[c])
		if k == 16 {
			if i == 0 {
				return 0, 0, errEmptyHexNum
			}
			return n, i, nil
		}
		if i >= 16 {
			return n, i, errTooLargeHexNum
		}
		n = (n << 4) | k
	}

	if i == 0 {
		return 0, 0, errEmptyHexNum
	}

	return n, i, nil
}

func formatHexUintFallback(buf *[16]byte, val int) int {
	if val < 0 {
		panic("BUG: int must be positive")
	}

	if val == 0 {
		buf[0] = '0'
		return 1
	}

	var tmp [16]byte
	i := len(tmp) - 1
	for {
		tmp[i] = lowerhex[val&0xf]
		val >>= 4
		if val == 0 {
			break
		}
		i--
	}

	n := copy(buf[:], tmp[i:])
	return n
}
