// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package framing_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/framing"
)

func TestLengthPrefixedFramer(t *testing.T) {
	t.Parallel()

	framer := framing.NewLengthPrefixedFramer(1024)
	buf := new(bytes.Buffer)

	payload := []byte("hello frame")
	n, err := framer.WriteFrame(buf, 0x01, payload)
	require.NoError(t, err)
	assert.Equal(t, 5+len(payload), n)

	flags, readPayload, err := framer.ReadFrame(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(0x01), flags)
	assert.Equal(t, "hello frame", string(readPayload))
}
