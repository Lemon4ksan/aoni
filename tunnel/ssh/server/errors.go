// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import "errors"

var (
	// ErrServerClosed indicates an operation was attempted on a closed SSH server.
	ErrServerClosed = errors.New("aoni ssh server: server is closed")

	// ErrInvalidPassword indicates authentication failure due to an incorrect password.
	ErrInvalidPassword = errors.New("aoni ssh server: invalid password")

	// ErrInvalidPublicKey indicates authentication failure due to an unaccepted public key.
	ErrInvalidPublicKey = errors.New("aoni ssh server: invalid public key")
)
