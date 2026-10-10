// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ipc provides utilities for IPC (Inter-Process Communication) over Unix domain sockets and Windows Named Pipes.
package ipc

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// NewUnixTransport creates an [http.RoundTripper] bound to a local Unix domain socket path.
func NewUnixTransport(socketPath string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives: false,
	}
}

func parseUnixURI(path string) (string, string, bool) {
	idx := strings.Index(path, ".sock")
	if idx == -1 {
		return "", "", false
	}
	sockEnd := idx + 5
	return path[:sockEnd], path[sockEnd:], true
}

func parseNpipeURI(path string) (string, string, bool) {
	idx := strings.Index(path, "/pipe/")
	if idx == -1 {
		return "", "", false
	}
	pipePrefixEnd := idx + len("/pipe/")
	slash := strings.IndexByte(path[pipePrefixEnd:], '/')
	if slash == -1 {
		return path, "/", true
	}
	pipeEnd := pipePrefixEnd + slash
	reqURI := path[pipeEnd:]
	if reqURI == "" {
		reqURI = "/"
	}
	return path[:pipeEnd], reqURI, true
}

// ParseIPCURI parses a URL carrying "unix://" or "npipe://" scheme,
// extracting the target socket path and relative HTTP request URI.
//
// Example:
//
//	"unix:///var/run/docker.sock/v1.41/containers/json"
//	 -> Socket: "/var/run/docker.sock", RequestURI: "/v1.41/containers/json"
func ParseIPCURI(rawURL string) (socketPath, reqURI string, isIPC bool) {
	if path, ok := strings.CutPrefix(rawURL, "unix://"); ok {
		if sock, req, ok := parseUnixURI(path); ok {
			return sock, req, true
		}
	}

	if path, ok := strings.CutPrefix(rawURL, "npipe://"); ok {
		if sock, req, ok := parseNpipeURI(path); ok {
			return sock, req, true
		}
	}

	return "", rawURL, false
}
