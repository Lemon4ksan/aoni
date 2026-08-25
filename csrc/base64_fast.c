// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// base64_encode_url encodes src into dst using RFC 4648 URL-safe alphabet without padding.
// Returns the number of bytes written to dst.
uint64_t base64_encode_url(const uint8_t *src, uint64_t len, uint8_t *dst, const uint8_t *charset) {
    uint64_t di = 0;
    uint64_t si = 0;

    while (si + 3 <= len) {
        uint32_t b0 = src[si];
        uint32_t b1 = src[si + 1];
        uint32_t b2 = src[si + 2];

        uint32_t triple = (b0 << 16) | (b1 << 8) | b2;

        dst[di]     = charset[(triple >> 18) & 0x3F];
        dst[di + 1] = charset[(triple >> 12) & 0x3F];
        dst[di + 2] = charset[(triple >> 6) & 0x3F];
        dst[di + 3] = charset[triple & 0x3F];

        si += 3;
        di += 4;
    }

    uint64_t remain = len - si;
    if (remain == 2) {
        uint32_t b0 = src[si];
        uint32_t b1 = src[si + 1];
        uint32_t triple = (b0 << 16) | (b1 << 8);

        dst[di]     = charset[(triple >> 18) & 0x3F];
        dst[di + 1] = charset[(triple >> 12) & 0x3F];
        dst[di + 2] = charset[(triple >> 6) & 0x3F];
        di += 3;
    } else if (remain == 1) {
        uint32_t b0 = src[si];
        uint32_t triple = b0 << 16;

        dst[di]     = charset[(triple >> 18) & 0x3F];
        dst[di + 1] = charset[(triple >> 12) & 0x3F];
        di += 2;
    }

    return di;
}
