// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows

package rio

import "errors"

var ErrRIONotSupported = errors.New("rio: Registered I/O extensions not supported on this OS")

type BufferRegistration struct {
	BufferID uintptr
	Data     []byte
}

func IsSupported() bool {
	return false
}

func RegisterBuffer(data []byte) (*BufferRegistration, error) {
	return nil, ErrRIONotSupported
}

func (b *BufferRegistration) Deregister() {}
