// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codec

import (
	"encoding/binary"

	"github.com/lemon4ksan/aoni/internal/simd"
)

// HeaderSlot stores a 16-bit offset index for HTTP headers (HAProxy HTX architecture).
// Occupies 8 bytes total (vs 48 bytes for two standard Go []byte slice headers).
type HeaderSlot struct {
	KeyPos uint16
	KeyLen uint16
	ValPos uint16
	ValLen uint16
}

// RequestHeaderIndex holds up to 32 indexed header slots on stack/cacheline memory (256 bytes total).
type RequestHeaderIndex struct {
	Slots [32]HeaderSlot
	Count uint16
}

// AddSlot records a header key-value offset range into the index.
func (idx *RequestHeaderIndex) AddSlot(keyPos, keyLen, valPos, valLen int) bool {
	if idx.Count >= 32 {
		return false
	}

	idx.Slots[idx.Count] = HeaderSlot{
		KeyPos: uint16(keyPos),
		KeyLen: uint16(keyLen),
		ValPos: uint16(valPos),
		ValLen: uint16(valLen),
	}
	idx.Count++

	return true
}

// GetHeader retrieves the byte slice value for a target key from raw buffer using uint64 word matching.
func (idx *RequestHeaderIndex) GetHeader(buf []byte, targetKey string) ([]byte, bool) {
	if len(targetKey) == 0 {
		return nil, false
	}

	var targetWord uint64
	if len(targetKey) >= 8 {
		targetWord = binary.LittleEndian.Uint64([]byte(targetKey[:8]))
	}

	for i := uint16(0); i < idx.Count; i++ {
		slot := idx.Slots[i]
		kStart := int(slot.KeyPos)
		kEnd := kStart + int(slot.KeyLen)

		if kEnd > len(buf) {
			continue
		}

		keySlice := buf[kStart:kEnd]

		if targetWord != 0 && len(keySlice) >= 8 {
			if !simd.MatchWord64(keySlice, targetWord) {
				continue
			}
		}

		if string(keySlice) == targetKey {
			vStart := int(slot.ValPos)
			vEnd := vStart + int(slot.ValLen)

			if vEnd <= len(buf) {
				return buf[vStart:vEnd], true
			}
		}
	}

	return nil, false
}
