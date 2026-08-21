// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

var _ Frame = &Settings{}

const (
	// defaultHeaderTableSize specifies the initial HPACK dynamic table size of 4,096 octets (RFC 9113 §4.3.1 & §6.5.2).
	defaultHeaderTableSize uint32 = 4096

	// defaultConcurrentStreams specifies the recommended initial concurrency limit of >= 100 streams (RFC 9113 §6.5.2).
	defaultConcurrentStreams uint32 = 100

	// defaultWindowSize specifies the initial flow-control window size of 65,535 octets (RFC 9113 §5.2.1, §6.5.2 & §6.9.2).
	defaultWindowSize uint32 = 1<<16 - 1

	// defaultDataFrameSize specifies the initial maximum frame payload size of 16,384 octets (2^14, RFC 9113 §4.2 & §6.5.2).
	defaultDataFrameSize uint32 = 1 << 14

	// maxFrameSize specifies the protocol maximum frame payload size of 16,777,215 octets (2^24 - 1, RFC 9113 §4.2 & §6.5.2).
	maxFrameSize uint32 = 1<<24 - 1
)

const (
	// HeaderTableSize allows the sender to inform the peer of the maximum HPACK table size (RFC 9113 §6.5.2: SETTINGS_HEADER_TABLE_SIZE, 0x1).
	HeaderTableSize uint16 = 0x1

	// EnablePush enables or disables HTTP/2 server push (RFC 9113 §6.5.2 & §8.4: SETTINGS_ENABLE_PUSH, 0x2).
	EnablePush uint16 = 0x2

	// MaxConcurrentStreams limits the maximum number of concurrent active streams (RFC 9113 §6.5.2: SETTINGS_MAX_CONCURRENT_STREAMS, 0x3).
	MaxConcurrentStreams uint16 = 0x3

	// MaxWindowSize indicates the sender's initial stream-level flow-control window (RFC 9113 §6.5.2: SETTINGS_INITIAL_WINDOW_SIZE, 0x4).
	MaxWindowSize uint16 = 0x4

	// MaxFrameSize indicates the size of the largest frame payload willing to receive (RFC 9113 §6.5.2: SETTINGS_MAX_FRAME_SIZE, 0x5).
	MaxFrameSize uint16 = 0x5

	// MaxHeaderListSize advises the peer of the maximum uncompressed field section size (RFC 9113 §6.5.2: SETTINGS_MAX_HEADER_LIST_SIZE, 0x6).
	MaxHeaderListSize uint16 = 0x6

	// EnableConnectProtocol enables Extended CONNECT for WebSockets over HTTP/2 (RFC 8441 §3: SETTINGS_ENABLE_CONNECT_PROTOCOL, 0x8).
	EnableConnectProtocol uint16 = 0x8
)

// Settings manages parameters negotiated between HTTP/2 endpoints (RFC 9113 §6.5).
type Settings struct {
	ack           bool
	rawSettings   []byte
	tableSize     uint32
	enablePush    bool
	maxStreams    uint32
	windowSize    uint32
	frameSize     uint32
	headerSize    uint32
	enableConnect bool
}

func (st *Settings) Type() FrameType { return FrameSettings }

func (st *Settings) Reset() {
	st.tableSize = defaultHeaderTableSize
	st.maxStreams = defaultConcurrentStreams
	st.windowSize = defaultWindowSize
	st.frameSize = defaultDataFrameSize
	st.enablePush = false
	st.enableConnect = false
	st.headerSize = 0
	st.rawSettings = st.rawSettings[:0]
	st.ack = false
}

func (st *Settings) CopyTo(dst *Settings) {
	dst.ack = st.ack
	dst.rawSettings = append(dst.rawSettings[:0], st.rawSettings...)
	dst.tableSize = st.tableSize
	dst.enablePush = st.enablePush
	dst.maxStreams = st.maxStreams
	dst.windowSize = st.windowSize
	dst.frameSize = st.frameSize
	dst.headerSize = st.headerSize
	dst.enableConnect = st.enableConnect
}

func (st *Settings) SetHeaderTableSize(size uint32)   { st.tableSize = size }
func (st *Settings) HeaderTableSize() uint32          { return st.tableSize }
func (st *Settings) SetPush(enabled bool)             { st.enablePush = enabled }
func (st *Settings) Push() bool                       { return st.enablePush }
func (st *Settings) SetMaxConcurrentStreams(m uint32) { st.maxStreams = m }
func (st *Settings) MaxConcurrentStreams() uint32     { return st.maxStreams }
func (st *Settings) SetMaxWindowSize(size uint32)     { st.windowSize = size }
func (st *Settings) MaxWindowSize() uint32            { return st.windowSize }
func (st *Settings) SetMaxFrameSize(size uint32)      { st.frameSize = size }
func (st *Settings) MaxFrameSize() uint32             { return st.frameSize }
func (st *Settings) SetMaxHeaderListSize(size uint32) { st.headerSize = size }
func (st *Settings) MaxHeaderListSize() uint32        { return st.headerSize }
func (st *Settings) SetEnableConnect(enabled bool)    { st.enableConnect = enabled }
func (st *Settings) EnableConnect() bool              { return st.enableConnect }
func (st *Settings) IsAck() bool                      { return st.ack }
func (st *Settings) SetAck(ack bool)                  { st.ack = ack }

func (st *Settings) Read(payload []byte) error {
	for i := 0; i+6 <= len(payload); i += 6 {
		key := uint16(payload[i])<<8 | uint16(payload[i+1])
		val := uint32(payload[i+2])<<24 | uint32(payload[i+3])<<16 | uint32(payload[i+4])<<8 | uint32(payload[i+5])

		if err := st.applySetting(key, val); err != nil {
			return err
		}
	}

	return nil
}

func (st *Settings) applySetting(key uint16, val uint32) error {
	switch key {
	case HeaderTableSize:
		// RFC 9113 §6.5.2: SETTINGS_HEADER_TABLE_SIZE
		st.tableSize = val
	case EnablePush:
		// RFC 9113 §6.5.2: SETTINGS_ENABLE_PUSH values other than 0 or 1 MUST be treated as PROTOCOL_ERROR.
		if val != 0 && val != 1 {
			return NewGoAwayError(ProtocolError, "wrong value for SETTINGS_ENABLE_PUSH (RFC 9113 §6.5.2)")
		}

		st.enablePush = val != 0

	case MaxConcurrentStreams:
		// RFC 9113 §6.5.2: SETTINGS_MAX_CONCURRENT_STREAMS
		st.maxStreams = val
	case MaxWindowSize:
		// RFC 9113 §6.5.2: Values above maximum flow-control window (2^31 - 1) MUST be treated as FLOW_CONTROL_ERROR.
		if val > 1<<31-1 {
			return NewGoAwayError(FlowControlError, "SETTINGS_INITIAL_WINDOW_SIZE above maximum (RFC 9113 §6.5.2)")
		}

		st.windowSize = val

	case MaxFrameSize:
		// RFC 9113 §6.5.2: Values outside 2^14 (16,384) to 2^24-1 (16,777,215) MUST be treated as PROTOCOL_ERROR.
		if val < 1<<14 || val > 1<<24-1 {
			return NewGoAwayError(ProtocolError, "wrong value for SETTINGS_MAX_FRAME_SIZE (RFC 9113 §6.5.2)")
		}

		st.frameSize = val

	case MaxHeaderListSize:
		// RFC 9113 §6.5.2: SETTINGS_MAX_HEADER_LIST_SIZE
		st.headerSize = val

	case EnableConnectProtocol:
		// RFC 8441 §3: SETTINGS_ENABLE_CONNECT_PROTOCOL values other than 0 or 1 MUST be treated as PROTOCOL_ERROR.
		if val != 0 && val != 1 {
			return NewGoAwayError(ProtocolError, "wrong value for SETTINGS_ENABLE_CONNECT_PROTOCOL (RFC 8441 §3)")
		}

		st.enableConnect = val == 1
	}

	return nil
}

func (st *Settings) Encode() {
	st.rawSettings = st.rawSettings[:0]
	st.appendSetting(HeaderTableSize, st.tableSize)

	if st.enablePush {
		st.appendSetting(EnablePush, 1)
	}

	st.appendSetting(MaxConcurrentStreams, st.maxStreams)
	st.appendSetting(MaxWindowSize, st.windowSize)
	st.appendSetting(MaxFrameSize, st.frameSize)
	st.appendSetting(MaxHeaderListSize, st.headerSize)

	if st.enableConnect {
		st.appendSetting(EnableConnectProtocol, 1)
	}
}

func (st *Settings) appendSetting(key uint16, val uint32) {
	if val == 0 && key != EnablePush && key != EnableConnectProtocol {
		return
	}

	st.rawSettings = append(st.rawSettings,
		byte(key>>8), byte(key),
		byte(val>>24), byte(val>>16), byte(val>>8), byte(val),
	)
}

func (st *Settings) Deserialize(fr *FrameHeader) error {
	// RFC 9113 §6.5: A SETTINGS frame with length other than a multiple of 6 octets MUST be treated as FRAME_SIZE_ERROR.
	if len(fr.payload)%6 != 0 {
		return NewGoAwayError(FrameSizeError, "wrong payload for settings (RFC 9113 §6.5)")
	}

	st.ack = fr.Flags().Has(FlagAck)
	// RFC 9113 §6.5: Receipt of a SETTINGS frame with ACK flag set and length != 0 MUST be treated as FRAME_SIZE_ERROR.
	if st.ack && len(fr.payload) > 0 {
		return NewGoAwayError(FrameSizeError, "settings with ack and payload (RFC 9113 §6.5)")
	}

	return st.Read(fr.payload)
}

func (st *Settings) Serialize(fr *FrameHeader) {
	if st.ack {
		fr.SetFlags(fr.Flags().Add(FlagAck))
		fr.payload = fr.payload[:0]

		return
	}

	st.Encode()
	fr.setPayload(st.rawSettings)
}
