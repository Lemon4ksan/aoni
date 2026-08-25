// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

typedef struct {
    uint16_t new_state;
    uint8_t  symbol;
    uint8_t  nb_bits;
} dec_symbol_t;

static inline uint16_t fse_get_bits_fast(uint64_t val, uint8_t *bits_read, uint8_t n) {
    uint8_t br = *bits_read;
    uint16_t v = (uint16_t)((val << (br & 63)) >> ((64 - n) & 63));
    *bits_read = br + n;
    return v;
}

// fse_decode_quad decodes 4 interleaved symbols (s1, s2, s1, s2) from the bit reservoir.
// Returns 4 packed bytes: (sym3 << 24) | (sym2 << 16) | (sym1 << 8) | sym0.
uint32_t fse_decode_quad(
    uint16_t *state1,
    uint16_t *state2,
    uint64_t val,
    uint8_t  *bits_read,
    const dec_symbol_t *dt
) {
    // 1. s1 next
    const dec_symbol_t *n1 = &dt[*state1];
    uint16_t low1 = fse_get_bits_fast(val, bits_read, n1->nb_bits);
    *state1 = n1->new_state + low1;
    uint8_t sym1 = n1->symbol;

    // 2. s2 next
    const dec_symbol_t *n2 = &dt[*state2];
    uint16_t low2 = fse_get_bits_fast(val, bits_read, n2->nb_bits);
    *state2 = n2->new_state + low2;
    uint8_t sym2 = n2->symbol;

    // 3. s1 next
    const dec_symbol_t *n3 = &dt[*state1];
    uint16_t low3 = fse_get_bits_fast(val, bits_read, n3->nb_bits);
    *state1 = n3->new_state + low3;
    uint8_t sym3 = n3->symbol;

    // 4. s2 next
    const dec_symbol_t *n4 = &dt[*state2];
    uint16_t low4 = fse_get_bits_fast(val, bits_read, n4->nb_bits);
    *state2 = n4->new_state + low4;
    uint8_t sym4 = n4->symbol;

    return ((uint32_t)sym4 << 24) | ((uint32_t)sym3 << 16) | ((uint32_t)sym2 << 8) | (uint32_t)sym1;
}
