// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package probe

import "golang.org/x/sys/unix"

func getSocketMTUOption(fd uintptr) (int, error) {
	return unix.GetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_MTU)
}
