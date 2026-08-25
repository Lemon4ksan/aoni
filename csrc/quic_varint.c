// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// quic_varint_len returns the encoded byte length (1, 2, 4, or 8) for a QUIC varint value (RFC 9000 §16).
// Pure branchless computation.
uint64_t quic_varint_len(uint64_t val) {
    uint64_t b = (val >= 64ULL) + (val >= 16384ULL) + (val >= 1073741824ULL);
    return 1ULL << b;
}

// quic_varint_parse decodes a QUIC variable-length integer (RFC 9000 §16).
// Returns 0 on success, or -1 if the buffer length is insufficient.
int64_t quic_varint_parse(const uint8_t *src, uint64_t len, uint64_t *val_out, uint64_t *consumed_out) {
    if (len == 0) {
        return -1;
    }

    uint8_t first = src[0];
    uint8_t tag = first >> 6;
    uint64_t need = 1ULL << tag;

    if (len < need) {
        return -1;
    }

    uint64_t val = 0;
    if (tag == 0) {
        val = (uint64_t)first;
    } else if (tag == 1) {
        val = (((uint64_t)(first & 0x3f)) << 8) | (uint64_t)src[1];
    } else if (tag == 2) {
        val = (((uint64_t)(first & 0x3f)) << 24) |
              (((uint64_t)src[1]) << 16) |
              (((uint64_t)src[2]) << 8) |
              ((uint64_t)src[3]);
    } else {
        uint64_t raw = 0;
        __builtin_memcpy(&raw, src, 8);
        #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
        raw = __builtin_bswap64(raw);
        #endif
        val = raw & 0x3fffffffffffffffULL;
    }

    *val_out = val;
    *consumed_out = need;
    return 0;
}

// quic_varint_append encodes val as a QUIC variable-length integer into dst (RFC 9000 §16).
// Returns the number of bytes written (1, 2, 4, or 8), or -1 if val exceeds 62 bits.
int64_t quic_varint_append(uint8_t *dst, uint64_t val) {
    if (val < 64ULL) {
        dst[0] = (uint8_t)val;
        return 1;
    }

    if (val < 16384ULL) {
        uint16_t v = (uint16_t)(val | 0x4000ULL);
        #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
        v = __builtin_bswap16(v);
        #endif
        __builtin_memcpy(dst, &v, 2);
        return 2;
    }

    if (val < 1073741824ULL) {
        uint32_t v = (uint32_t)(val | 0x80000000ULL);
        #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
        v = __builtin_bswap32(v);
        #endif
        __builtin_memcpy(dst, &v, 4);
        return 4;
    }

    if (val > 0x3fffffffffffffffULL) {
        return -1;
    }

    uint64_t v = val | 0xc000000000000000ULL;
    #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
    v = __builtin_bswap64(v);
    #endif
    __builtin_memcpy(dst, &v, 8);
    return 8;
}
