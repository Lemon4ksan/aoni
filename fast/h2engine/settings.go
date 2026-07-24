// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

var _ Frame = &Settings{}

const (
	defaultHeaderTableSize   uint32 = 4096
	defaultConcurrentStreams uint32 = 100
	defaultWindowSize        uint32 = 1<<16 - 1
	defaultDataFrameSize     uint32 = 1 << 14
	maxFrameSize             uint32 = 1<<24 - 1
)

const (
	HeaderTableSize      uint16 = 0x1
	EnablePush           uint16 = 0x2
	MaxConcurrentStreams uint16 = 0x3
	MaxWindowSize        uint16 = 0x4
	MaxFrameSize         uint16 = 0x5
	MaxHeaderListSize    uint16 = 0x6
)

// Settings manages parameters negotiated between HTTP/2 endpoints.
type Settings struct {
	ack         bool
	rawSettings []byte
	tableSize   uint32
	enablePush  bool
	maxStreams  uint32
	windowSize  uint32
	frameSize   uint32
	headerSize  uint32
}

func (st *Settings) Type() FrameType { return FrameSettings }

func (st *Settings) Reset() {
	st.tableSize = defaultHeaderTableSize
	st.maxStreams = defaultConcurrentStreams
	st.windowSize = defaultWindowSize
	st.frameSize = defaultDataFrameSize
	st.enablePush = false
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
		st.tableSize = val
	case EnablePush:
		if val != 0 && val != 1 {
			return NewGoAwayError(ProtocolError, "wrong value for SETTINGS_ENABLE_PUSH")
		}

		st.enablePush = val != 0
	case MaxConcurrentStreams:
		st.maxStreams = val
	case MaxWindowSize:
		if val > 1<<31-1 {
			return NewGoAwayError(FlowControlError, "SETTINGS_INITIAL_WINDOW_SIZE above maximum")
		}

		st.windowSize = val
	case MaxFrameSize:
		if val < 1<<14 || val > 1<<24-1 {
			return NewGoAwayError(ProtocolError, "wrong value for SETTINGS_MAX_FRAME_SIZE")
		}

		st.frameSize = val
	case MaxHeaderListSize:
		st.headerSize = val
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
}

func (st *Settings) appendSetting(key uint16, val uint32) {
	if val == 0 && key != EnablePush {
		return
	}

	st.rawSettings = append(st.rawSettings,
		byte(key>>8), byte(key),
		byte(val>>24), byte(val>>16), byte(val>>8), byte(val),
	)
}

func (st *Settings) Deserialize(fr *FrameHeader) error {
	if len(fr.payload)%6 != 0 {
		return NewGoAwayError(FrameSizeError, "wrong payload for settings")
	}

	st.ack = fr.Flags().Has(FlagAck)
	if st.ack && len(fr.payload) > 0 {
		return NewGoAwayError(FrameSizeError, "settings with ack and payload")
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
