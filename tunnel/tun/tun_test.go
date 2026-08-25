// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tun_test

import (
	"bytes"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/tunnel/tun"
)

type mockAdapter struct {
	buf  bytes.Buffer
	name string
}

func (m *mockAdapter) Read(p []byte) (n int, err error) {
	return m.buf.Read(p)
}

func (m *mockAdapter) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockAdapter) Close() error {
	return nil
}

func (m *mockAdapter) Name() string {
	return m.name
}

func TestAdapterInterface(t *testing.T) {
	t.Parallel()

	var adapter tun.Adapter = &mockAdapter{name: "mock0"}
	assert.Equal(t, "mock0", adapter.Name())

	n, err := adapter.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	buf := make([]byte, 5)
	n, err = adapter.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf))
	assert.NoError(t, adapter.Close())
}
