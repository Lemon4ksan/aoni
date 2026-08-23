// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.25 && !go1.26

package handshake

import "crypto/tls"

const quicErrorEvent tls.QUICEventKind = -1

func extractQUICEventError(tls.QUICEvent) error {
	return nil
}
