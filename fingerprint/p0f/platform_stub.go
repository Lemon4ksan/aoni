// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !unix && !windows

package p0f

import "syscall"

func applySignature(_ syscall.RawConn, _ *Signature) {}
