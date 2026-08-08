// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package probe

import "golang.org/x/sys/unix"

func setSocketTTL(fd uintptr, ttl int, isV6 bool) error {
	sfd := int(fd)
	if isV6 {
		return unix.SetsockoptInt(sfd, unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, ttl)
	}

	return unix.SetsockoptInt(sfd, unix.IPPROTO_IP, unix.IP_TTL, ttl)
}
