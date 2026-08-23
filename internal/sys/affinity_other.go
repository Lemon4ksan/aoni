// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows && !linux

package sys

func setThreadAffinityMask(cores []int) {
	// Native OS affinity handling for Darwin / FreeBSD / others where thread affinity is unexposed or no-op
}
