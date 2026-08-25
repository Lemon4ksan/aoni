// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// crc32_ieee_update computes or updates the CRC-32 IEEE 802.3 checksum (polynomial 0xEDB88320).
uint32_t crc32_ieee_update(uint32_t crc, const uint8_t *data, uint64_t len, const uint32_t *table) {
    crc = ~crc;
    const uint8_t *p = data;
    const uint8_t *end = data + len;

    while (p != end) {
        crc = table[(crc ^ *p++) & 0xFF] ^ (crc >> 8);
    }

    return ~crc;
}
