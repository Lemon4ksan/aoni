// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import "errors"

var (
	ErrServerClosed           = errors.New("aoni h3engine: server closed HTTP/3 connection")
	ErrFrameUnexpected        = errors.New("aoni h3engine: received unexpected frame type")
	ErrHeaderTooLarge         = errors.New("aoni h3engine: response headers exceed size limit")
	ErrStreamClosed           = errors.New("aoni h3engine: stream closed prematurely")
	ErrInvalidStreamType      = errors.New("aoni h3engine: invalid unidirectional stream type")
	ErrMissingSettings        = errors.New("aoni h3engine: server did not send initial SETTINGS frame")
	ErrDuplicateControlStream = errors.New("aoni h3engine: duplicate control stream received")
	ErrClosedCriticalStream   = errors.New("aoni h3engine: critical unidirectional stream was closed")
	ErrQPACKDecompressFailed  = errors.New("aoni h3engine: QPACK header decompression failed")
	ErrMissingStatusHeader    = errors.New("aoni h3engine: response missing mandatory :status header")
	ErrInvalidHostHeader      = errors.New("aoni h3engine: invalid Host or :authority header")
)
