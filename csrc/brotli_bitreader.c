// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// brotli_fill_bit_window refills the 64-bit accumulator if bit_pos >= 32.
void brotli_fill_bit_window(uint64_t *val, uint64_t *bit_pos, const uint8_t *input, uint64_t *byte_pos) {
    if (*bit_pos >= 32) {
        *val >>= 32;
        *bit_pos ^= 32;
        uint32_t b32;
        __builtin_memcpy(&b32, input + *byte_pos, 4);
        *val |= ((uint64_t)b32) << 32;
        *byte_pos += 4;
    }
}

// brotli_read_bits refills the bit accumulator and extracts nbits in a single operation.
uint64_t brotli_read_bits(uint64_t *val, uint64_t *bit_pos, const uint8_t *input, uint64_t *byte_pos, uint64_t nbits) {
    if (*bit_pos >= 32) {
        *val >>= 32;
        *bit_pos ^= 32;
        uint32_t b32;
        __builtin_memcpy(&b32, input + *byte_pos, 4);
        *val |= ((uint64_t)b32) << 32;
        *byte_pos += 4;
    }

    uint64_t mask = (1ULL << nbits) - 1ULL;
    uint64_t res = (*val >> *bit_pos) & mask;
    *bit_pos += nbits;
    return res;
}
