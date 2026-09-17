// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ipc_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/netutil/ipc"
)

func TestParseIPCURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rawURL     string
		wantSocket string
		wantReqURI string
		wantIsIPC  bool
	}{
		{
			name:       "docker_unix_socket_with_api_path",
			rawURL:     "unix:///var/run/docker.sock/v1.41/containers/json",
			wantSocket: "/var/run/docker.sock",
			wantReqURI: "/v1.41/containers/json",
			wantIsIPC:  true,
		},
		{
			name:       "unix_socket_exact",
			rawURL:     "unix:///tmp/app.sock",
			wantSocket: "/tmp/app.sock",
			wantReqURI: "",
			wantIsIPC:  true,
		},
		{
			name:       "unix_scheme_without_sock_extension",
			rawURL:     "unix:///var/run/custom_pipe/api",
			wantSocket: "",
			wantReqURI: "unix:///var/run/custom_pipe/api",
			wantIsIPC:  false,
		},
		{
			name:       "windows_named_pipe_standard",
			rawURL:     "npipe:////./pipe/docker_engine",
			wantSocket: "//./pipe/docker_engine",
			wantReqURI: "/",
			wantIsIPC:  true,
		},
		{
			name:       "windows_named_pipe_without_pipe_in_path",
			rawURL:     "npipe://localhost/other/path",
			wantSocket: "",
			wantReqURI: "npipe://localhost/other/path",
			wantIsIPC:  false,
		},
		{
			name:       "regular_http_url",
			rawURL:     "http://example.com/api/v1",
			wantSocket: "",
			wantReqURI: "http://example.com/api/v1",
			wantIsIPC:  false,
		},
		{
			name:       "empty_url",
			rawURL:     "",
			wantSocket: "",
			wantReqURI: "",
			wantIsIPC:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sock, uri, isIPC := ipc.ParseIPCURI(tt.rawURL)
			assert.Equal(t, tt.wantSocket, sock)
			assert.Equal(t, tt.wantReqURI, uri)
			assert.Equal(t, tt.wantIsIPC, isIPC)
		})
	}
}

func TestNewUnixTransport(t *testing.T) {
	t.Parallel()

	transport := ipc.NewUnixTransport("/var/run/docker.sock")
	require.NotNil(t, transport)
	assert.False(t, transport.DisableKeepAlives)
	assert.NotNil(t, transport.DialContext)
}
