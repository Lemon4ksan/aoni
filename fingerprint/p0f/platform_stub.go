// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux && !darwin && !freebsd && !openbsd && !windows

package p0f

import (
	"syscall"

	"github.com/lemon4ksan/aoni/internal/sysnet"
)

func applySignature(raw syscall.RawConn, sig *Signature) {
	hasDF := hasQuirk(sig.Quirks, "df") || hasQuirk(sig.Quirks, "df+")
	setWin := sig.WindowType == WindowNormal
	sysnet.ApplyP0fSignature(raw, sig.TTL, sig.WindowSize, setWin, hasDF)
}
