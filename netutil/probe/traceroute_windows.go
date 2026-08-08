// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package probe

import "golang.org/x/sys/windows"

func setSocketTTL(fd uintptr, ttl int, isV6 bool) error {
	h := windows.Handle(fd)
	if isV6 {
		return windows.SetsockoptInt(h, windows.IPPROTO_IPV6, windows.IPV6_UNICAST_HOPS, ttl)
	}

	return windows.SetsockoptInt(h, windows.IPPROTO_IP, windows.IP_TTL, ttl)
}
