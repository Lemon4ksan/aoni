// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// quic_hp_mask_apply applies the 5-byte header protection mask to the first byte and packet number bytes (RFC 9001 §5.4).
// In-place, zero-allocation transformation.
void quic_hp_mask_apply(const uint8_t *mask, uint8_t *first_byte, uint8_t *pn_bytes, uint64_t pn_len, uint64_t is_long) {
    uint8_t fb_mask = is_long ? 0x0F : 0x1F;
    *first_byte ^= (mask[0] & fb_mask);

    if (pn_len >= 4) {
        uint32_t m;
        __builtin_memcpy(&m, mask + 1, 4);
        uint32_t p;
        __builtin_memcpy(&p, pn_bytes, 4);
        p ^= m;
        __builtin_memcpy(pn_bytes, &p, 4);
        return;
    }

    for (uint64_t i = 0; i < pn_len; i++) {
        pn_bytes[i] ^= mask[i + 1];
    }
}
