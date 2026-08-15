// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !unix && !windows

package sysnet

func tuneSocketFD(_ uintptr) {}

func tuneSocketFlagsFD(_ uintptr, _ uint64) {}
