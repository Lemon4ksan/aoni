// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qpack

import (
	"errors"
	"io"
)

var (
	errIntegerOverflow = errors.New("qpack: integer overflow")
	errInvalidInteger  = errors.New("qpack: invalid prefix integer")
)

func appendInt(dst []byte, prefixLen uint8, val uint64) []byte {
	if prefixLen > 8 || prefixLen == 0 {
		panic("invalid prefix length")
	}

	maxPrefix := uint64((1 << prefixLen) - 1)
	if val < maxPrefix {
		dst[len(dst)-1] |= byte(val)
		return dst
	}

	dst[len(dst)-1] |= byte(maxPrefix)
	val -= maxPrefix

	for val >= 128 {
		dst = append(dst, byte((val&0x7f)|0x80))
		val >>= 7
	}

	return append(dst, byte(val))
}

func readInt(prefixLen uint8, data []byte) (uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, io.ErrUnexpectedEOF
	}

	if prefixLen > 8 || prefixLen == 0 {
		return 0, 0, errInvalidInteger
	}

	maxPrefix := uint64((1 << prefixLen) - 1)
	prefix := uint64(data[0]) & maxPrefix

	if prefix < maxPrefix {
		return prefix, 1, nil
	}

	val := maxPrefix
	shift := 0
	idx := 1

	for {
		if idx >= len(data) {
			return 0, 0, io.ErrUnexpectedEOF
		}

		b := data[idx]
		idx++

		if shift >= 63 {
			return 0, 0, errIntegerOverflow
		}

		val += uint64(b&0x7f) << shift
		shift += 7

		if b&0x80 == 0 {
			break
		}
	}

	return val, idx, nil
}

func appendString(dst []byte, s string) []byte {
	hLen := huffmanLen(s)
	if hLen < len(s) {
		dst = append(dst, 0x80) // Huffman bit = 1
		dst = appendInt(dst, 7, uint64(hLen))
		return appendHuffman(dst, s)
	}

	dst = append(dst, 0x00) // Huffman bit = 0
	dst = appendInt(dst, 7, uint64(len(s)))

	return append(dst, s...)
}

func readString(data []byte) (string, int, error) {
	if len(data) == 0 {
		return "", 0, io.ErrUnexpectedEOF
	}

	isHuffman := (data[0] & 0x80) != 0

	strLen, n, err := readInt(7, data)
	if err != nil {
		return "", 0, err
	}

	if int(strLen) < 0 || n+int(strLen) > len(data) {
		return "", 0, io.ErrUnexpectedEOF
	}

	raw := data[n : n+int(strLen)]
	totalConsumed := n + int(strLen)

	if isHuffman {
		s, err := decodeHuffman(raw)
		if err != nil {
			return "", 0, err
		}

		return s, totalConsumed, nil
	}

	return string(raw), totalConsumed, nil
}
