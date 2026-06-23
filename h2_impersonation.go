// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"net"
	"strings"

	"github.com/lemon4ksan/aoni/profiles"
)

// HTTP2Settings holds the full set of HTTP/2 connection parameters
// for browser-grade frame impersonation. Each field maps directly to
// an HTTP/2 SETTINGS frame parameter or PRIORITY frame value.
type HTTP2Settings struct {
	HeaderTableSize      uint32
	EnablePush           uint32
	MaxConcurrentStreams uint32
	InitialWindowSize    uint32
	MaxFrameSize         uint32
	MaxHeaderListSize    uint32
	ConnectionFlow       uint32
	InitialStreamID      uint32
	PriorityStreamDep    uint32
	PriorityExclusive    bool
	PriorityWeight       uint8
}

// H2SettingsFromProfile populates HTTP2Settings from a profiles.H2Settings.
func H2SettingsFromProfile(s profiles.H2Settings) HTTP2Settings {
	return HTTP2Settings{
		HeaderTableSize:      s.HeaderTableSize,
		EnablePush:           s.EnablePush,
		MaxConcurrentStreams: s.MaxConcurrentStreams,
		InitialWindowSize:    s.InitialWindowSize,
		MaxFrameSize:         s.MaxFrameSize,
		MaxHeaderListSize:    s.MaxHeaderListSize,
		ConnectionFlow:       s.ConnectionFlow,
		InitialStreamID:      s.InitialStreamID,
		PriorityStreamDep:    s.PriorityStreamDep,
		PriorityExclusive:    s.PriorityExclusive,
		PriorityWeight:       s.PriorityWeight,
	}
}

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
