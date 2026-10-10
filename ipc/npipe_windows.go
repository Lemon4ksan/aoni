// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package ipc

import (
	"context"
	"net"
	"net/http"
	"os"

	"golang.org/x/sys/windows"
)

// NewNamedPipeTransport creates an [http.RoundTripper] bound to a Windows Named Pipe (e.g. "\\.\pipe\docker_engine").
func NewNamedPipeTransport(pipePath string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialWindowsPipe(ctx, pipePath)
		},
	}
}

func dialWindowsPipe(_ context.Context, path string) (net.Conn, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(handle), path)

	conn, err := net.FileConn(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return conn, nil
}
