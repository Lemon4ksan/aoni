// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport_test

import (
	"bytes"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/internal/transport"
)

func TestLengthPrefixedFramer(t *testing.T) {
	t.Parallel()

	framer := transport.NewLengthPrefixedFramer(1024)
	payload := []byte("hello grpc-web")

	var buf bytes.Buffer

	n, err := framer.WriteFrame(&buf, 0x01, payload)
	assert.NoError(t, err)
	assert.Equal(t, 5+len(payload), n)

	flags, readPayload, err := framer.ReadFrame(&buf)
	assert.NoError(t, err)
	assert.Equal(t, byte(0x01), flags)
	assert.Equal(t, payload, readPayload)
}
