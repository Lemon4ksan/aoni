// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.26

package handshake

import "crypto/fips140"

func withoutFIPSEnforcement(f func()) {
	fips140.WithoutEnforcement(f)
}
