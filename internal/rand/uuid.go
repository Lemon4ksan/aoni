// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rand

import (
	"encoding/binary"
	"time"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

const hexDigits = "0123456789abcdef"

// AppendUUIDv7 appends a 36-byte time-ordered UUIDv7 (RFC 9562 §5.7) to dst without heap allocations.
// Format: xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx
func AppendUUIDv7(dst []byte, now time.Time) []byte {
	var raw [16]byte

	// 1. 48-bit timestamp in milliseconds
	ms := uint64(now.UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)

	// 2. 12-bit random + 4-bit version (0111)
	r1 := Uint32()
	raw[6] = 0x70 | byte((r1>>8)&0x0F) // Version 7
	raw[7] = byte(r1)

	// 3. 2-bit variant (10) + 62-bit random
	r2 := Uint64()
	binary.BigEndian.PutUint64(raw[8:], r2)
	raw[8] = (raw[8] & 0x3F) | 0x80 // Variant 10

	// 4. Hex encoding with hyphens directly into dst
	start := len(dst)
	dst = append(dst, make([]byte, 36)...)
	buf := dst[start:]

	buf[0] = hexDigits[raw[0]>>4]
	buf[1] = hexDigits[raw[0]&0x0F]
	buf[2] = hexDigits[raw[1]>>4]
	buf[3] = hexDigits[raw[1]&0x0F]
	buf[4] = hexDigits[raw[2]>>4]
	buf[5] = hexDigits[raw[2]&0x0F]
	buf[6] = hexDigits[raw[3]>>4]
	buf[7] = hexDigits[raw[3]&0x0F]
	buf[8] = '-'

	buf[9] = hexDigits[raw[4]>>4]
	buf[10] = hexDigits[raw[4]&0x0F]
	buf[11] = hexDigits[raw[5]>>4]
	buf[12] = hexDigits[raw[5]&0x0F]
	buf[13] = '-'

	buf[14] = hexDigits[raw[6]>>4]
	buf[15] = hexDigits[raw[6]&0x0F]
	buf[16] = hexDigits[raw[7]>>4]
	buf[17] = hexDigits[raw[7]&0x0F]
	buf[18] = '-'

	buf[19] = hexDigits[raw[8]>>4]
	buf[20] = hexDigits[raw[8]&0x0F]
	buf[21] = hexDigits[raw[9]>>4]
	buf[22] = hexDigits[raw[9]&0x0F]
	buf[23] = '-'

	buf[24] = hexDigits[raw[10]>>4]
	buf[25] = hexDigits[raw[10]&0x0F]
	buf[26] = hexDigits[raw[11]>>4]
	buf[27] = hexDigits[raw[11]&0x0F]
	buf[28] = hexDigits[raw[12]>>4]
	buf[29] = hexDigits[raw[12]&0x0F]
	buf[30] = hexDigits[raw[13]>>4]
	buf[31] = hexDigits[raw[13]&0x0F]
	buf[32] = hexDigits[raw[14]>>4]
	buf[33] = hexDigits[raw[14]&0x0F]
	buf[34] = hexDigits[raw[15]>>4]
	buf[35] = hexDigits[raw[15]&0x0F]

	return dst
}

// UUIDv7 generates a time-ordered 36-byte UUIDv7 string with zero heap allocations.
func UUIDv7() string {
	var buf [36]byte

	res := AppendUUIDv7(buf[:0], time.Now())

	return bytesconv.B2S(res)
}
