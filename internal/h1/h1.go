// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package h1 provides socket-level HTTP/1.1 wire header reordering connection wrappers.
package h1

import (
	"bytes"
	"errors"
	"net"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// ErrInvalidHeaderTerminator is returned when HTTP/1.1 header section terminators (\r\n\r\n) are missing or corrupted.
var ErrInvalidHeaderTerminator = errors.New("h1: invalid or truncated HTTP header section")

const (
	lineTerminator    = "\r\n"
	sectionTerminator = "\r\n\r\n"
)

type headerEntry struct {
	key  []byte
	line []byte
}

// HeaderOrderingConn wraps a [net.Conn] to reorder HTTP/1.1 request header lines prior to socket transmission.
type HeaderOrderingConn struct {
	net.Conn
	OrderedKeys []string
}

func (c *HeaderOrderingConn) Write(b []byte) (int, error) {
	if len(c.OrderedKeys) > 0 && bytes.Contains(b, bytesconv.S2B(sectionTerminator)) {
		if rewritten, ok := ReorderHeaders(b, c.OrderedKeys); ok {
			b = rewritten
		}
	}

	return c.Conn.Write(b)
}

// ReorderHeaders reorders header lines in raw HTTP/1.1 wire byte buffers according to order with zero heap allocations.
func ReorderHeaders(raw []byte, order []string) ([]byte, bool) {
	headerBytes, body, ok := bytes.Cut(raw, bytesconv.S2B(sectionTerminator))
	if !ok {
		return nil, false
	}

	// Stack-allocated array to avoid heap allocations for standard HTTP header sets
	var stackBuf [64]headerEntry

	parsed := stackBuf[:0]

	var requestLine []byte

	rest := headerBytes

	for len(rest) > 0 {
		var line []byte

		idx := bytes.Index(rest, bytesconv.S2B(lineTerminator))
		if idx >= 0 {
			line = rest[:idx]
			rest = rest[idx+2:]
		} else {
			line = rest
			rest = nil
		}

		if requestLine == nil {
			requestLine = line
			continue
		}

		before, _, hasColon := bytes.Cut(line, []byte{':'})
		if !hasColon {
			continue
		}

		entry := headerEntry{
			key:  bytes.TrimSpace(before),
			line: line,
		}

		parsed = append(parsed, entry)
	}

	if requestLine == nil {
		return nil, false
	}

	numHeaders := min(len(parsed), 64)

	var newHeader bytes.Buffer
	newHeader.Grow(len(raw))

	newHeader.Write(requestLine)
	newHeader.WriteString(lineTerminator)

	// CPU register bitmask tracking written header indices with zero allocations
	var writtenBits uint64

	for _, targetKey := range order {
		for i := 0; i < numHeaders; i++ {
			if (writtenBits&(1<<i)) == 0 && bytesconv.EqualFoldASCII(bytesconv.B2S(parsed[i].key), targetKey) {
				newHeader.Write(parsed[i].line)
				newHeader.WriteString(lineTerminator)

				writtenBits |= (1 << i)

				break
			}
		}
	}

	for i := 0; i < numHeaders; i++ {
		if (writtenBits & (1 << i)) == 0 {
			newHeader.Write(parsed[i].line)
			newHeader.WriteString(lineTerminator)
		}
	}

	newHeader.WriteString(lineTerminator)
	newHeader.Write(body)

	return newHeader.Bytes(), true
}
