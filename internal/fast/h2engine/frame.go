// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"strconv"
	"sync"

	"github.com/lemon4ksan/aoni/internal/offheap"
)

// FrameType identifies the protocol function of an HTTP/2 frame (RFC 7540 Section 6).
type FrameType uint8

const (
	FrameData         FrameType = 0x0
	FrameHeaders      FrameType = 0x1
	FramePriority     FrameType = 0x2
	FrameResetStream  FrameType = 0x3
	FrameSettings     FrameType = 0x4
	FramePushPromise  FrameType = 0x5
	FramePing         FrameType = 0x6
	FrameGoAway       FrameType = 0x7
	FrameWindowUpdate FrameType = 0x8
	FrameContinuation FrameType = 0x9
)

func (ft FrameType) String() string {
	switch ft {
	case FrameData:
		return "FrameData"
	case FrameHeaders:
		return "FrameHeaders"
	case FramePriority:
		return "FramePriority"
	case FrameResetStream:
		return "FrameResetStream"
	case FrameSettings:
		return "FrameSettings"
	case FramePushPromise:
		return "FramePushPromise"
	case FramePing:
		return "FramePing"
	case FrameGoAway:
		return "FrameGoAway"
	case FrameWindowUpdate:
		return "FrameWindowUpdate"
	case FrameContinuation:
		return "FrameContinuation"
	default:
		return strconv.Itoa(int(ft))
	}
}

// FrameFlags defines bit flags controlling payload parsing and stream semantics.
type FrameFlags uint8

// Has reports whether target bit flag set is enabled.
func (flags FrameFlags) Has(target FrameFlags) bool {
	return flags&target == target
}

// Add enables target bit flag.
func (flags FrameFlags) Add(target FrameFlags) FrameFlags {
	return flags | target
}

// Del clears target bit flag.
func (flags FrameFlags) Del(target FrameFlags) FrameFlags {
	return flags &^ target
}

// Frame defines the serialization interface satisfied by all HTTP/2 protocol frames.
type Frame interface {
	Type() FrameType
	Reset()
	Serialize(header *FrameHeader)
	Deserialize(header *FrameHeader) error
}

var framePools = func() [FrameContinuation + 1]*sync.Pool {
	var pools [FrameContinuation + 1]*sync.Pool

	pools[FrameData] = &sync.Pool{New: func() any { return &Data{} }}
	pools[FrameHeaders] = &sync.Pool{New: func() any { return &Headers{} }}
	pools[FramePriority] = &sync.Pool{New: func() any { return &Priority{} }}
	pools[FrameResetStream] = &sync.Pool{New: func() any { return &RstStream{} }}
	pools[FrameSettings] = &sync.Pool{New: func() any { return &Settings{} }}
	pools[FramePushPromise] = &sync.Pool{New: func() any { return &PushPromise{} }}
	pools[FramePing] = &sync.Pool{New: func() any { return &Ping{} }}
	pools[FrameGoAway] = &sync.Pool{New: func() any { return &GoAway{} }}
	pools[FrameWindowUpdate] = &sync.Pool{New: func() any { return &WindowUpdate{} }}
	pools[FrameContinuation] = &sync.Pool{New: func() any { return &Continuation{} }}

	return pools
}()

// AcquireFrame fetches a clean Frame instance from memory pools for frameType.
func AcquireFrame(frameType FrameType) Frame {
	fr := framePools[frameType].Get().(Frame)
	fr.Reset()

	return fr
}

// AcquireFrameInArena fetches a clean Frame instance.
// For POD frames (Ping, Priority, WindowUpdate, RstStream), if arena is non-nil,
// it uses offheap.AllocStruct[T](arena) for 0-alloc single-cycle off-heap construction.
func AcquireFrameInArena(arena *offheap.Arena, frameType FrameType) Frame {
	if arena != nil {
		switch frameType {
		case FramePing:
			p := offheap.AllocStruct[Ping](arena)
			p.Reset()
			return p
		case FramePriority:
			pr := offheap.AllocStruct[Priority](arena)
			pr.Reset()
			return pr
		case FrameWindowUpdate:
			wu := offheap.AllocStruct[WindowUpdate](arena)
			wu.Reset()
			return wu
		case FrameResetStream:
			rs := offheap.AllocStruct[RstStream](arena)
			rs.Reset()
			return rs
		}
	}

	return AcquireFrame(frameType)
}

// ReleaseFrame returns a Frame instance back to memory pools.
func ReleaseFrame(fr Frame) {
	if fr != nil && int(fr.Type()) >= 0 && int(fr.Type()) < len(framePools) {
		framePools[fr.Type()].Put(fr)
	}
}
