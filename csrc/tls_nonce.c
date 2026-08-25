// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// tls13_compute_nonce computes the 12-byte per-record AEAD nonce (RFC 8446 §5.3).
// dst receives 12 bytes: iv[0..3] followed by (iv[4..11] ^ seq_be).
void tls13_compute_nonce(const uint8_t *iv, uint64_t seq, uint8_t *dst) {
    uint32_t hi;
    uint64_t lo;

    __builtin_memcpy(&hi, iv, 4);
    __builtin_memcpy(&lo, iv + 4, 8);

    lo ^= __builtin_bswap64(seq);

    __builtin_memcpy(dst, &hi, 4);
    __builtin_memcpy(dst + 4, &lo, 8);
}
