// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux && !windows && !darwin

package quic

import (
	"syscall"
)

func setDF(syscall.RawConn) (bool, error) {
	// no-op on unsupported platforms
	return false, nil
}

func isSendMsgSizeErr(err error) bool {
	// to be implemented for more specific platforms
	return false
}

func isRecvMsgSizeErr(err error) bool {
	// to be implemented for more specific platforms
	return false
}
