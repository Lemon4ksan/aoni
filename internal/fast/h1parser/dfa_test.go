// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/fast/h1parser"
)

func TestParseHTTP1SinglePass(t *testing.T) {
	raw := []byte("GET /status HTTP/1.1\r\nHost: localhost:8080\r\nContent-Type: application/json\r\n\r\n")

	method, uri, proto, htx, parsed, ok := h1parser.ParseHTTP1SinglePass(raw)
	assert.True(t, ok)
	assert.Equal(t, "GET", string(method))
	assert.Equal(t, "/status", string(uri))
	assert.Equal(t, "HTTP/1.1", string(proto))
	assert.Greater(t, parsed, 0)

	hostVal, found := htx.GetHeader(raw, "Host")
	assert.True(t, found)
	assert.Equal(t, "localhost:8080", string(hostVal))
}
