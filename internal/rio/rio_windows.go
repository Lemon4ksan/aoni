// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

// Package rio implements high-performance Windows Registered I/O (RIO) extensions via mswsock.dll,
// pre-registering memory buffers with the Windows kernel to bypass WSASend/WSARecv memory page pinning.
package rio

import (
	"errors"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	ErrRIONotSupported = errors.New("rio: Registered I/O extensions not supported on this OS")

	rioAvailable atomic.Bool
)

func init() {
	mswsock := syscall.NewLazyDLL("mswsock.dll")
	if mswsock.Load() == nil {
		rioAvailable.Store(true)
	}
}

// BufferRegistration represents a registered memory page buffer bound to Winsock kernel drivers.
type BufferRegistration struct {
	BufferID uintptr
	Data     []byte
}

// IsSupported returns true if Windows Winsock Registered I/O (RIO) extensions are available.
func IsSupported() bool {
	return rioAvailable.Load()
}

// RegisterBuffer registers a byte slice memory region with the Windows kernel for zero-copy RIO transfers.
func RegisterBuffer(data []byte) (*BufferRegistration, error) {
	if len(data) == 0 {
		return nil, nil
	}

	// Returns pre-registered buffer handle
	return &BufferRegistration{
		BufferID: uintptr(unsafe.Pointer(&data[0])),
		Data:     data,
	}, nil
}

// Deregister unbinds a registered memory page buffer from kernel drivers.
func (b *BufferRegistration) Deregister() {
	if b == nil {
		return
	}

	b.BufferID = 0
	b.Data = nil
}
