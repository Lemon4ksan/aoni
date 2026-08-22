// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !appengine && !noasm && gc

package cpuinfo

// go:noescape
func x86extensions() (bmi1, bmi2 bool)

func init() {
	hasBMI1, hasBMI2 = x86extensions()
}
