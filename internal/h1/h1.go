// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package h1 provides TCP-level HTTP/1.1 header reordering connection wrappers.
package h1

import (
	"bytes"
	"errors"
	"net"
	"strings"
)

const (
	lineTerminator    = "\r\n"
	sectionTerminator = lineTerminator + lineTerminator
)

type header struct {
	key  string
	line []byte
}

// HeaderOrderingConn wraps a [net.Conn] to reorder HTTP/1.1 headers before
// they reach the wire. It operates at the TCP level, sitting between the raw
// socket and the TLS layer (e.g. [tls.Conn] or [utls.UConn]).
//
// This placement is critical: TLS calls Write() on the wrapped connection
// with plaintext data before encrypting. So HeaderOrderingConn sees and
// reorders plaintext HTTP headers, not encrypted TLS records.
//
// Wrapping order: TCP → HeaderOrderingConn → TLS → Go HTTP client
type HeaderOrderingConn struct {
	net.Conn
	OrderedKeys []string
}

// Write intercepts serialized HTTP/1.1 requests and reorders headers
// according to the configured order. Detection is based on the presence
// of the HTTP header terminator \r\n\r\n in the written bytes.
func (c *HeaderOrderingConn) Write(b []byte) (n int, err error) {
	if len(c.OrderedKeys) > 0 && bytes.Contains(b, []byte(sectionTerminator)) {
		if rewritten, ok := ReorderHeaders(b, c.OrderedKeys); ok {
			b = rewritten
		}
	}

	return c.Conn.Write(b)
}

// ReorderHeaders reorders the HTTP headers in the given raw HTTP/1.1 request
// according to the specified order. Returns the reordered bytes and a success flag.
func ReorderHeaders(raw []byte, order []string) ([]byte, bool) {
	body, lines, err := splitHeader(raw)
	if err != nil {
		return nil, false
	}

	request, rawHeaders := lines[0], lines[1:]

	parsed := make([]header, 0, len(rawHeaders))
	headersMap := make(map[string][]byte, len(rawHeaders))

	for _, h := range rawHeaders {
		before, _, ok := bytes.Cut(h, []byte{':'})
		if !ok {
			continue
		}

		key := strings.ToLower(string(bytes.TrimSpace(before)))
		parsed = append(parsed, header{key: key, line: h})
		headersMap[key] = h
	}

	var newHeader bytes.Buffer
	newHeader.Write(request)
	newHeader.Write([]byte(lineTerminator))

	written := make(map[string]bool, len(order))
	for _, key := range order {
		lowerKey := strings.ToLower(key)
		if line, ok := headersMap[lowerKey]; ok {
			newHeader.Write(line)
			newHeader.Write([]byte(lineTerminator))

			written[lowerKey] = true
		}
	}

	for _, h := range parsed {
		if !written[h.key] {
			newHeader.Write(h.line)
			newHeader.Write([]byte(lineTerminator))
		}
	}

	newHeader.Write([]byte(lineTerminator))
	newHeader.Write(body)

	return newHeader.Bytes(), true
}

func splitHeader(raw []byte) (body []byte, lines [][]byte, err error) {
	header, body, ok := bytes.Cut(raw, []byte(sectionTerminator))
	if !ok {
		err = errors.New("invalid header")
		return body, lines, err
	}

	lines = bytes.Split(header, []byte(lineTerminator))
	if len(lines) < 2 {
		err = errors.New("invalid header")
	}

	return body, lines, err
}
