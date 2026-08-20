// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sftp

import "errors"

var (
	// ErrInvalidTargetFile is returned when a target SCP filename contains control characters (\r, \n, \x00).
	ErrInvalidTargetFile = errors.New("aoni/ssh/sftp: invalid control characters in target filename")

	// ErrInvalidChunkSize is returned when parallel transfer chunkSize is less than or equal to zero.
	ErrInvalidChunkSize = errors.New("aoni/ssh/sftp: parallel chunk size must be greater than zero")

	// ErrParallelTransferFailed is returned when one or more chunks fail during parallel file transfer.
	ErrParallelTransferFailed = errors.New("aoni/ssh/sftp: parallel file transfer failed")

	// ErrClientClosed is returned when operations are attempted on an inactive or nil SSH client.
	ErrClientClosed = errors.New("aoni/ssh/sftp: ssh client session is closed")
)
