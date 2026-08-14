// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows

package sysnet

import "errors"

var ErrRIONotSupported = errors.New("rio: Registered I/O extensions not supported on this OS")

// BufferRegistration is a stub for non-Windows platforms.
type BufferRegistration struct {
	BufferID uintptr
	Data     []byte
}

// IsRIOSupported returns false on non-Windows platforms.
func IsRIOSupported() bool {
	return false
}

// RegisterBuffer returns ErrRIONotSupported on non-Windows platforms.
func RegisterBuffer(data []byte) (*BufferRegistration, error) {
	return nil, ErrRIONotSupported
}

// Deregister is a no-op on non-Windows platforms.
func (b *BufferRegistration) Deregister() {}
