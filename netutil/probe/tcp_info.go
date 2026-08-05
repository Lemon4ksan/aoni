// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// FILE: probe/tcp_info.go

package probe

import "time"

// TCPInfo holds raw kernel TCP connection metrics extracted directly from the OS socket.
type TCPInfo struct {
	RTT          time.Duration
	RTTVar       time.Duration
	RttMin       time.Duration
	SndCwnd      uint32
	Retransmits  uint32
	TotalRetrans uint32
}
