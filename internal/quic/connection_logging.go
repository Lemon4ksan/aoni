// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/internal/wire"
)

func (c *Conn) logLongHeaderPacket(p *longHeaderPacket) {
	if c.logger.Debug() {
		p.header.Log(c.logger)

		if p.ack != nil {
			wire.LogFrame(c.logger, p.ack, true)
		}

		for _, frame := range p.frames {
			wire.LogFrame(c.logger, frame.Frame, true)
		}

		for _, frame := range p.streamFrames {
			wire.LogFrame(c.logger, frame.Frame, true)
		}
	}
}

func (c *Conn) logShortHeaderPacket(p shortHeaderPacket, ecn protocol.ECN, size protocol.ByteCount, isCoalesced bool) {
	if c.logger.Debug() {
		if !isCoalesced {
			c.logger.Debugf(
				"-> Sending packet %d (%d bytes) for connection %s, 1-RTT (ECN: %s)",
				p.PacketNumber,
				size,
				c.logID,
				ecn,
			)
		}

		wire.LogShortHeader(c.logger, p.DestConnID, p.PacketNumber, p.PacketNumberLen, p.KeyPhase)

		if p.Ack != nil {
			wire.LogFrame(c.logger, p.Ack, true)
		}

		for _, f := range p.Frames {
			wire.LogFrame(c.logger, f.Frame, true)
		}

		for _, f := range p.StreamFrames {
			wire.LogFrame(c.logger, f.Frame, true)
		}
	}
}

func (c *Conn) logCoalescedPacket(packet *coalescedPacket, ecn protocol.ECN) {
	if c.logger.Debug() {
		// There's a short period between dropping both Initial and Handshake keys and completion of the handshake,
		// during which we might call PackCoalescedPacket but just pack a short header packet.
		if len(packet.longHdrPackets) == 0 && packet.shortHdrPacket != nil {
			c.logShortHeaderPacket(
				*packet.shortHdrPacket,
				ecn,
				packet.shortHdrPacket.Length,
				false,
			)

			return
		}

		if len(packet.longHdrPackets) > 1 {
			c.logger.Debugf(
				"-> Sending coalesced packet (%d parts, %d bytes) for connection %s",
				len(packet.longHdrPackets),
				packet.buffer.Len(),
				c.logID,
			)
		} else if len(packet.longHdrPackets) == 1 {
			c.logger.Debugf(
				"-> Sending packet %d (%d bytes) for connection %s, %s",
				packet.longHdrPackets[0].header.PacketNumber,
				packet.buffer.Len(),
				c.logID,
				packet.longHdrPackets[0].EncryptionLevel(),
			)
		}
	}

	for _, p := range packet.longHdrPackets {
		c.logLongHeaderPacket(p)
	}

	if p := packet.shortHdrPacket; p != nil {
		c.logShortHeaderPacket(*p, ecn, p.Length, true)
	}
}
