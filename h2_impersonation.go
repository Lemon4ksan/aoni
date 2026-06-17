// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"net"
	"strings"
)

type headerOrderingConn struct {
	net.Conn
	orderedKeys []string
}

func (c *headerOrderingConn) Write(b []byte) (n int, err error) {
	if len(c.orderedKeys) > 0 && (bytes.HasPrefix(b, []byte("GET ")) ||
		bytes.HasPrefix(b, []byte("POST ")) || bytes.HasPrefix(b, []byte("PUT ")) ||
		bytes.HasPrefix(b, []byte("DELETE ")) || bytes.HasPrefix(b, []byte("PATCH "))) {
		if rewritten, ok := reorderHTTP1Headers(b, c.orderedKeys); ok {
			b = rewritten
		}
	}

	return c.Conn.Write(b)
}

func reorderHTTP1Headers(raw []byte, order []string) ([]byte, bool) {
	headerEnd := bytes.Index(raw, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		return nil, false
	}

	headerPart := raw[:headerEnd]
	bodyPart := raw[headerEnd:]

	lines := bytes.Split(headerPart, []byte("\r\n"))
	if len(lines) < 2 {
		return nil, false
	}

	requestLine := lines[0]
	headerLines := lines[1:]

	headersMap := make(map[string][]byte)
	for _, line := range headerLines {
		before, _, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			continue
		}

		key := strings.ToLower(string(bytes.TrimSpace(before)))
		headersMap[key] = line
	}

	var newHeaderPart bytes.Buffer
	newHeaderPart.Write(requestLine)
	newHeaderPart.Write([]byte("\r\n"))

	written := make(map[string]bool)
	for _, key := range order {
		lowerKey := strings.ToLower(key)
		if line, ok := headersMap[lowerKey]; ok {
			newHeaderPart.Write(line)
			newHeaderPart.Write([]byte("\r\n"))

			written[lowerKey] = true
		}
	}

	for _, line := range headerLines {
		before, _, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			continue
		}

		key := strings.ToLower(string(bytes.TrimSpace(before)))
		if !written[key] {
			newHeaderPart.Write(line)
			newHeaderPart.Write([]byte("\r\n"))
		}
	}

	newHeaderPart.Write(bodyPart)

	return newHeaderPart.Bytes(), true
}

// HTTP2Settings holds the settings for an HTTP/2 connection.
type HTTP2Settings struct {
	MaxHeaderListSize uint32
	InitialWindowSize uint32
}
