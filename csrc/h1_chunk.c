// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// h1_parse_hex_uint parses an ASCII hex string into out_val (RFC 9112 §7.1).
// Returns the number of parsed bytes, or -1 on invalid character/overflow.
int64_t h1_parse_hex_uint(const uint8_t *src, uint64_t len, uint64_t *out_val) {
    if (!len) return -1;

    uint64_t val = 0;
    uint64_t i = 0;

    for (; i < len; ++i) {
        uint8_t c = src[i];
        uint8_t d;

        if (c >= '0' && c <= '9') {
            d = c - '0';
        } else if (c >= 'a' && c <= 'f') {
            d = c - 'a' + 10;
        } else if (c >= 'A' && c <= 'F') {
            d = c - 'A' + 10;
        } else {
            if (i == 0) return -1;
            break;
        }

        if (i >= 16) return -1; // overflow check for 64-bit int

        val = (val << 4) | (uint64_t)d;
    }

    *out_val = val;
    return (int64_t)i;
}

// h1_format_hex_uint formats val as a lower-case hexadecimal string into dst (RFC 9112 §7.1).
// Returns the number of bytes written to dst.
uint64_t h1_format_hex_uint(uint8_t *dst, uint64_t val) {
    if (!val) {
        dst[0] = '0';
        return 1;
    }

    uint8_t buf[16];
    int idx = 15;

    while (val > 0) {
        uint8_t nib = (uint8_t)(val & 0x0F);
        buf[idx--] = (nib < 10) ? ('0' + nib) : ('a' + (nib - 10));
        val >>= 4;
    }

    uint64_t count = (uint64_t)(15 - idx);
    for (uint64_t i = 0; i < count; ++i) {
        dst[i] = buf[idx + 1 + i];
    }
    return count;
}

// h1_format_chunk_header formats val as "<hex_len>\r\n" into dst.
// Returns the number of bytes written to dst.
uint64_t h1_format_chunk_header(uint8_t *dst, uint64_t val) {
    uint64_t n = h1_format_hex_uint(dst, val);
    dst[n] = '\r';
    dst[n + 1] = '\n';
    return n + 2;
}
