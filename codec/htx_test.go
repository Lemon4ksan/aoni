// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/codec"
)

func TestHTX_HeaderIndex(t *testing.T) {
	buf := []byte("GET /api/v1 HTTP/1.1\r\nHost: example.com\r\nContent-Type: application/json\r\n\r\n")

	var htx codec.RequestHeaderIndex
	assert.True(t, htx.AddSlot(22, 4, 28, 11))  // Host: example.com
	assert.True(t, htx.AddSlot(41, 12, 55, 16)) // Content-Type: application/json

	val, ok := htx.GetHeader(buf, "Host")
	assert.True(t, ok)
	assert.Equal(t, "example.com", string(val))

	valType, okType := htx.GetHeader(buf, "Content-Type")
	assert.True(t, okType)
	assert.Equal(t, "application/json", string(valType))
}
