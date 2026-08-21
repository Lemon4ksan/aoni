// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	fnetutil "github.com/lemon4ksan/foundation/net/netutil"
)

// CleanHost normalizes a host string for network resolution, HTTP headers, and TLS SNI.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/netutil].
func CleanHost(host string) string {
	return fnetutil.CleanHost(host)
}

// CleanHostPort splits addr into host and port, normalizes the host via [CleanHost],
// and returns the sanitized host and port components.
func CleanHostPort(addr string) (host, port string) {
	return fnetutil.CleanHostPort(addr)
}
