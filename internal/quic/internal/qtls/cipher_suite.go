// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qtls

// SetCipherSuite is a no-op preserved for testing compatibility.
func SetCipherSuite(_ uint16) (reset func()) {
	return func() {}
}
