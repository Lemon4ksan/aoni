// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ipc_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/ipc"
	"github.com/lemon4ksan/foundation/testing/assert"
)

func TestParseIPCURI(t *testing.T) {
	t.Parallel()

	// 1. Unix socket with path
	sock, req, ok := ipc.ParseIPCURI("unix:///var/run/docker.sock/v1.41/containers/json")
	assert.True(t, ok)
	assert.Equal(t, "/var/run/docker.sock", sock)
	assert.Equal(t, "/v1.41/containers/json", req)

	// 2. Named pipe with path
	sock, req, ok = ipc.ParseIPCURI("npipe:////./pipe/docker_engine/v1.41/containers/json")
	assert.True(t, ok)
	assert.Equal(t, "//./pipe/docker_engine", sock)
	assert.Equal(t, "/v1.41/containers/json", req)

	// 3. Named pipe without trailing path
	sock, req, ok = ipc.ParseIPCURI("npipe:////./pipe/docker_engine")
	assert.True(t, ok)
	assert.Equal(t, "//./pipe/docker_engine", sock)
	assert.Equal(t, "/", req)

	// 4. Non-IPC URI
	sock, req, ok = ipc.ParseIPCURI("https://example.com/api")
	assert.False(t, ok)
	assert.Equal(t, "", sock)
	assert.Equal(t, "https://example.com/api", req)
}
