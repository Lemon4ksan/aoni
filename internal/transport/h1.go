// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"bytes"
	"errors"
	"net"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/simd"
)

// ErrInvalidHeaderTerminator is returned when HTTP/1.1 header section terminators (\r\n\r\n) are missing or corrupted.
var ErrInvalidHeaderTerminator = errors.New("transport: invalid or truncated HTTP header section")

// RFC 9112 §2.1 & §2.2: Standard HTTP/1.1 CRLF line terminators.
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

// ReorderHeaders reorders header lines in raw HTTP/1.1 wire byte buffers according to order with zero heap allocations (RFC 9112 §2.1 & §3.2).
func ReorderHeaders(raw []byte, order []string) ([]byte, bool) {
	headerBytes, body, ok := bytes.Cut(raw, bytesconv.S2B(sectionTerminator))
	if !ok {
		return nil, false
	}

	var stackBuf [64]headerEntry

	parsed := stackBuf[:0]

	var requestLine []byte

	rest := headerBytes

	for len(rest) > 0 {
		var line []byte

		idx := simd.IndexByteVector(rest, '\n')
		if idx >= 0 {
			if idx > 0 && rest[idx-1] == '\r' {
				line = rest[:idx-1]
			} else {
				line = rest[:idx]
			}

			rest = rest[idx+1:]
		} else {
			line = rest
			rest = nil
		}

		if requestLine == nil {
			requestLine = line
			continue
		}

		colonIdx := simd.IndexByteVector(line, ':')
		if colonIdx < 0 {
			continue
		}

		entry := headerEntry{
			key:  bytes.TrimSpace(line[:colonIdx]),
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

	var writtenBits uint64

	for _, targetKey := range order {
		for i := 0; i < numHeaders; i++ {
			if (writtenBits&(1<<i)) == 0 && (bytesconv.EqualFoldASCII(bytesconv.B2S(parsed[i].key), targetKey) ||
				(targetKey == ":authority" && bytesconv.EqualFoldASCII(bytesconv.B2S(parsed[i].key), "host"))) {
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
