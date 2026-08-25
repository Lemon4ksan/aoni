// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// prefix_int_encode encodes val with a prefix_len-bit prefix into dst (RFC 7541 §5.1 & RFC 9204 §4.1.1).
// The first byte of dst may already contain prefix bits in upper bits.
// Returns the number of bytes added to dst.
uint64_t prefix_int_encode(uint8_t *dst, uint64_t prefix_len, uint64_t val) {
    if (prefix_len == 0 || prefix_len > 8) {
        return 0;
    }

    uint64_t max_prefix = (1ULL << prefix_len) - 1ULL;
    if (val < max_prefix) {
        dst[0] |= (uint8_t)val;
        return 1;
    }

    dst[0] |= (uint8_t)max_prefix;
    val -= max_prefix;

    uint64_t idx = 1;
    while (val >= 128ULL) {
        dst[idx++] = (uint8_t)((val & 0x7fULL) | 0x80ULL);
        val >>= 7ULL;
    }
    dst[idx++] = (uint8_t)val;
    return idx;
}

// prefix_int_decode decodes a prefix_len-bit prefix integer from src (RFC 7541 §5.1 & RFC 9204 §4.1.1).
// Writes decoded integer into *out_val and consumed byte count into *out_consumed.
// Returns 0 on success, -1 on EOF, -2 on overflow.
int64_t prefix_int_decode(const uint8_t *src, uint64_t src_len, uint64_t prefix_len, uint64_t *out_val, uint64_t *out_consumed) {
    if (src_len == 0) {
        return -1; // EOF
    }
    if (prefix_len == 0 || prefix_len > 8) {
        return -3; // invalid prefix
    }

    uint64_t max_prefix = (1ULL << prefix_len) - 1ULL;
    uint64_t val = (uint64_t)src[0] & max_prefix;

    if (val < max_prefix) {
        *out_val = val;
        *out_consumed = 1;
        return 0;
    }

    uint64_t shift = 0;
    uint64_t idx = 1;

    while (idx < src_len) {
        uint8_t b = src[idx++];
        if (shift >= 63) {
            return -2; // integer overflow
        }

        val += ((uint64_t)(b & 0x7f)) << shift;
        shift += 7;

        if ((b & 0x80) == 0) {
            *out_val = val;
            *out_consumed = idx;
            return 0;
        }
    }

    return -1; // unexpected EOF
}
