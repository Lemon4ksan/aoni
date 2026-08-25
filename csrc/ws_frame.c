// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// ws_mask_xor applies the 4-byte WebSocket XOR mask key to payload in 64-bit unrolled chunks (RFC 6455 §5.3).
void ws_mask_xor(uint8_t *payload, uint64_t len, uint32_t mask_key) {
    if (!len) return;

    uint64_t mask64 = ((uint64_t)mask_key) | (((uint64_t)mask_key) << 32);
    uint64_t n64 = len >> 3;
    uint64_t *p64 = (uint64_t *)payload;

    for (uint64_t i = 0; i < n64; ++i) {
        p64[i] ^= mask64;
    }

    uint8_t *tail = payload + (n64 << 3);
    uint8_t rem = (uint8_t)(len & 7);
    const uint8_t *m = (const uint8_t *)&mask_key;

    for (uint8_t i = 0; i < rem; ++i) {
        tail[i] ^= m[i & 3];
    }
}

// ws_build_frame_header serializes a WebSocket frame header (RFC 6455 §5.2).
// Returns the written header length (2, 4, or 10 octets).
uint64_t ws_build_frame_header(uint8_t *dst, uint64_t opcode, uint64_t len, uint64_t compress, uint64_t is_client) {
    uint8_t b0 = 0x80 | (uint8_t)(opcode & 0x0F);
    if (compress) {
        b0 |= 0x40; // RSV1 bit
    }
    dst[0] = b0;

    uint8_t mask_bit = is_client ? 0x80 : 0x00;

    if (len < 126) {
        dst[1] = mask_bit | (uint8_t)len;
        return 2;
    } else if (len <= 0xFFFFULL) {
        dst[1] = mask_bit | 126;
        dst[2] = (uint8_t)(len >> 8);
        dst[3] = (uint8_t)len;
        return 4;
    } else {
        dst[1] = mask_bit | 127;
        #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
        uint64_t sw = __builtin_bswap64(len);
        #else
        uint64_t sw = len;
        #endif
        __builtin_memcpy(dst + 2, &sw, 8);
        return 10;
    }
}
