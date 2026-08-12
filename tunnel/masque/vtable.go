// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

// PacketHandler handles an IP packet payload.
type PacketHandler func(packet []byte) error

// IPProtocolVTable is a 256-element Jump Table VTable for Layer 3 IP protocol dispatching.
type IPProtocolVTable struct {
	handlers [256]PacketHandler
}

// NewIPProtocolVTable constructs a new [IPProtocolVTable] initialized with default handlers.
func NewIPProtocolVTable() *IPProtocolVTable {
	return &IPProtocolVTable{}
}

// Register assigns a custom [PacketHandler] to the specified IP protocol byte (e.g. 6 for TCP, 17 for UDP).
func (v *IPProtocolVTable) Register(proto byte, handler PacketHandler) {
	v.handlers[proto] = handler
}

// DispatchIPPacket executes monomorphic hot-path dispatching for TCP (6) and UDP (17),
// falling back to VTable jump-table lookup for cold protocols (ICMP, ICMPv6, IGMP, etc.).
func (v *IPProtocolVTable) DispatchIPPacket(packet []byte) error {
	if len(packet) < 20 {
		return ErrInvalidIPHeader
	}

	var proto byte

	ipVer := packet[0] >> 4

	switch ipVer {
	case 4:
		proto = packet[9]
	case 6:
		if len(packet) < 40 {
			return ErrInvalidIPHeader
		}

		proto = packet[6]

	default:
		return ErrInvalidIPHeader
	}

	// Monomorphic Hot-Path (99%+ of Layer 3 IP traffic): TCP (6) & UDP (17)
	switch proto {
	case 6:
		if h := v.handlers[6]; h != nil {
			return h(packet)
		}
	case 17:
		if h := v.handlers[17]; h != nil {
			return h(packet)
		}
	}

	// Cold-Path VTable (Jump Table) fallback
	h := v.handlers[proto]
	if h != nil {
		return h(packet)
	}

	return ErrUnhandledProtocol
}
