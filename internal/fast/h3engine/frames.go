// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine

import (
	"errors"
	"io"

	"github.com/quic-go/quic-go/quicvarint"
)

var ErrH3SettingsError = errors.New("aoni/h3engine: reserved H2 setting ID in H3 SETTINGS frame")

const (
	FrameTypeData        uint64 = 0x00
	FrameTypeHeaders     uint64 = 0x01
	FrameTypeCancelPush  uint64 = 0x03
	FrameTypeSettings    uint64 = 0x04
	FrameTypePushPromise uint64 = 0x05
	FrameTypeGoAway      uint64 = 0x07
	FrameTypeMaxPushID   uint64 = 0x0D
)

const (
	StreamTypeControl      uint64 = 0x00
	StreamTypePush         uint64 = 0x01
	StreamTypeQPACKEncoder uint64 = 0x02
	StreamTypeQPACKDecoder uint64 = 0x03
)

const (
	SettingQpackMaxTableCapacity uint64 = 0x01
	SettingMaxFieldSectionSize   uint64 = 0x06
	SettingQpackBlockedStreams   uint64 = 0x07
	SettingEnableConnectProtocol uint64 = 0x08
	SettingH3Datagram            uint64 = 0x33
)

// Settings encapsulates HTTP/3 connection parameters negotiated during control stream setup.
type Settings struct {
	MaxFieldSectionSize int64
	QpackMaxTableCap    uint64
	QpackBlockedStreams uint64
	EnableDatagrams     bool
	EnableConnect       bool
	Other               map[uint64]uint64
}

// Encode serializes Settings into a binary H3 SETTINGS frame payload.
func (s *Settings) Encode() []byte {
	var payload []byte

	if s.MaxFieldSectionSize >= 0 {
		payload = quicvarint.Append(payload, SettingMaxFieldSectionSize)
		payload = quicvarint.Append(payload, uint64(s.MaxFieldSectionSize))
	}

	if s.QpackMaxTableCap > 0 {
		payload = quicvarint.Append(payload, SettingQpackMaxTableCapacity)
		payload = quicvarint.Append(payload, s.QpackMaxTableCap)
	}

	if s.QpackBlockedStreams > 0 {
		payload = quicvarint.Append(payload, SettingQpackBlockedStreams)
		payload = quicvarint.Append(payload, s.QpackBlockedStreams)
	}

	if s.EnableDatagrams {
		payload = quicvarint.Append(payload, SettingH3Datagram)
		payload = quicvarint.Append(payload, 1)
	}

	if s.EnableConnect {
		payload = quicvarint.Append(payload, SettingEnableConnectProtocol)
		payload = quicvarint.Append(payload, 1)
	}

	for k, v := range s.Other {
		payload = quicvarint.Append(payload, k)
		payload = quicvarint.Append(payload, v)
	}

	var frame []byte

	frame = quicvarint.Append(frame, FrameTypeSettings)
	frame = quicvarint.Append(frame, uint64(len(payload)))

	return append(frame, payload...)
}

// DecodeSettings decodes an incoming H3 SETTINGS frame and checks for reserved H2 settings
func DecodeSettings(r io.Reader, payloadLen uint64) (*Settings, error) {
	lr := io.LimitReader(r, int64(payloadLen))
	qr := quicvarint.NewReader(lr)

	st := &Settings{
		Other: make(map[uint64]uint64),
	}

	for {
		id, err := quicvarint.Read(qr)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, err
		}

		val, err := quicvarint.Read(qr)
		if err != nil {
			return nil, err
		}

		// RFC 9114 Section 7.2.4.1: reserved H2 settings (0x00, 0x02, 0x03, 0x04, 0x05)
		switch id {
		case 0x00, 0x02, 0x03, 0x04, 0x05:
			return nil, ErrH3SettingsError

		case SettingMaxFieldSectionSize:
			st.MaxFieldSectionSize = int64(val)
		case SettingQpackMaxTableCapacity:
			st.QpackMaxTableCap = val
		case SettingQpackBlockedStreams:
			st.QpackBlockedStreams = val
		case SettingH3Datagram:
			st.EnableDatagrams = (val == 1)
		case SettingEnableConnectProtocol:
			st.EnableConnect = (val == 1)
		default:
			st.Other[id] = val
		}
	}

	return st, nil
}

// ReadFrameHeader decodes the QUIC varint frame type and payload length from stream r.
func ReadFrameHeader(r quicvarint.Reader) (frameType, payloadLen uint64, err error) {
	frameType, err = quicvarint.Read(r)
	if err != nil {
		return 0, 0, err
	}

	payloadLen, err = quicvarint.Read(r)
	if err != nil {
		return 0, 0, err
	}

	return frameType, payloadLen, nil
}

func appendHeadersHeader(b []byte, length uint64) []byte {
	b = quicvarint.Append(b, FrameTypeHeaders)
	return quicvarint.Append(b, length)
}

func appendDataHeader(b []byte, length uint64) []byte {
	b = quicvarint.Append(b, FrameTypeData)
	return quicvarint.Append(b, length)
}
