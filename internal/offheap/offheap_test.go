// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/offheap"
)

func TestOffHeapBuffer_WriteRead(t *testing.T) {
	buf, err := offheap.NewBuffer(64 * 1024)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Release()

	n, err := buf.WriteString("GET /api/v1 HTTP/1.1\r\n\r\n")
	assert.NoError(t, err)
	assert.Equal(t, 24, n)
	assert.Equal(t, 24, buf.Len())

	bView := buf.Bytes()
	assert.Equal(t, "GET /api/v1 HTTP/1.1\r\n\r\n", string(bView))

	readDst := make([]byte, 64)
	rn, rErr := buf.Read(readDst)
	assert.NoError(t, rErr)
	assert.Equal(t, 24, rn)

	buf.Reset()
	assert.Equal(t, 0, buf.Len())
}

func TestScopeRAII_AllocAndPanicResilience(t *testing.T) {
	scopeErr := offheap.Scope(2*1024*1024, func(arena *offheap.Arena) {
		require.NotNil(t, arena)

		b1 := arena.AllocBuffer(1024)
		require.NotNil(t, b1)

		_, wErr := b1.WriteString("hello offheap arena")
		assert.NoError(t, wErr)
		assert.Equal(t, "hello offheap arena", string(b1.Bytes()))
	})
	assert.NoError(t, scopeErr)

	// Verify panic resilience
	defer func() {
		r := recover()
		assert.NotNil(t, r, "panic should be recovered")
	}()

	_ = offheap.Scope(1024*1024, func(_ *offheap.Arena) {
		panic("testing panic resilience inside scope")
	})
}
