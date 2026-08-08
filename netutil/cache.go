// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import "crypto/tls"

// ResolveStdSessionCache adapts an aoni SessionCache into a standard [tls.ClientSessionCache].
func ResolveStdSessionCache(cache any) tls.ClientSessionCache {
	if cache == nil {
		return nil
	}

	if provider, ok := cache.(interface{ StdTLSSessionCache() tls.ClientSessionCache }); ok {
		return provider.StdTLSSessionCache()
	}

	if stdCache, ok := cache.(tls.ClientSessionCache); ok {
		return stdCache
	}

	return nil
}
