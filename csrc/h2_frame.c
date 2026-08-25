// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// h2_frame_header_pack serializes an HTTP/2 9-byte frame header (RFC 9113 §4.1).
// Packs 24-bit length, 8-bit type, 8-bit flags, and 31-bit stream ID into dst.
void h2_frame_header_pack(uint8_t *dst, uint64_t length, uint64_t kind, uint64_t flags, uint64_t stream) {
    uint32_t sid = (uint32_t)(stream & 0x7FFFFFFFULL);
    uint64_t w = ((length & 0xFFFFFFULL) << 40) |
                 ((kind & 0xFFULL) << 32) |
                 ((flags & 0xFFULL) << 24) |
                 (((uint64_t)sid) >> 8);

    #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
    w = __builtin_bswap64(w);
    #endif

    __builtin_memcpy(dst, &w, 8);
    dst[8] = (uint8_t)sid;
}

// h2_frame_header_unpack parses an HTTP/2 9-byte frame header from src (RFC 9113 §4.1).
// Extracts 24-bit length, 8-bit type, 8-bit flags, and 31-bit stream ID in a single 64-bit load.
void h2_frame_header_unpack(const uint8_t *src, uint64_t *out_len, uint64_t *out_kind, uint64_t *out_flags, uint64_t *out_stream) {
    uint64_t w;
    __builtin_memcpy(&w, src, 8);

    #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
    w = __builtin_bswap64(w);
    #endif

    *out_len = w >> 40;
    *out_kind = (w >> 32) & 0xFFULL;
    *out_flags = (w >> 24) & 0xFFULL;
    *out_stream = (((w & 0xFFFFFFULL) << 8) | (uint64_t)src[8]) & 0x7FFFFFFFULL;
}
