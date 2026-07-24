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
var ErrInvalidHeaderTerminator = errors.New("aoni h1: invalid or truncated HTTP header section")

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

// ReorderHeaders reorders header lines in raw HTTP/1.1 wire byte buffers according to order.
func ReorderHeaders(raw []byte, order []string) ([]byte, bool) {
	body, lines, err := splitHeader(raw)
	if err != nil || len(lines) < 2 {
		return nil, false
	}

	requestLine, rawHeaders := lines[0], lines[1:]
	parsed := make([]headerEntry, 0, len(rawHeaders))

	for _, h := range rawHeaders {
		before, _, ok := bytes.Cut(h, []byte{':'})
		if !ok {
			continue
		}

		parsed = append(parsed, headerEntry{
			key:  bytes.TrimSpace(before),
			line: h,
		})
	}

	var newHeader bytes.Buffer
	newHeader.Grow(len(raw))
	newHeader.Write(requestLine)
	newHeader.WriteString(lineTerminator)

	written := make([]bool, len(parsed))

	for _, targetKey := range order {
		for i, h := range parsed {
			if !written[i] && bytesconv.EqualFoldASCII(bytesconv.B2S(h.key), targetKey) {
				newHeader.Write(h.line)
				newHeader.WriteString(lineTerminator)

				written[i] = true

				break
			}
		}
	}

	for i, h := range parsed {
		if !written[i] {
			newHeader.Write(h.line)
			newHeader.WriteString(lineTerminator)
		}
	}

	newHeader.WriteString(lineTerminator)
	newHeader.Write(body)

	return newHeader.Bytes(), true
}

func splitHeader(raw []byte) ([]byte, [][]byte, error) {
	headerBytes, body, ok := bytes.Cut(raw, bytesconv.S2B(sectionTerminator))
	if !ok {
		return nil, nil, ErrInvalidHeaderTerminator
	}

	lines := bytes.Split(headerBytes, bytesconv.S2B(lineTerminator))
	if len(lines) < 2 {
		return nil, nil, ErrInvalidHeaderTerminator
	}

	return body, lines, nil
}
