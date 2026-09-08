// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package ipc_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/ipc"
)

func TestNewNamedPipeTransport(t *testing.T) {
	t.Parallel()

	transport := ipc.NewNamedPipeTransport(`\\.\pipe\docker_engine`)
	require.NotNil(t, transport)
	assert.NotNil(t, transport.DialContext)
}
