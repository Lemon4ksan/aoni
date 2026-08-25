// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

typedef struct {
    uint32_t nbits;
    uint32_t code;
} huffman_sym;

typedef struct {
    uint16_t fstate;
    uint8_t flags;
    uint8_t sym;
} huffman_decode_node;

// hpack_huffman_encode_count calculates the exact encoded byte length of src (RFC 7541 §5.2).
uint64_t hpack_huffman_encode_count(const uint8_t *src, uint64_t len, const huffman_sym *sym_table) {
    uint64_t nbits = 0;
    for (uint64_t i = 0; i < len; ++i) {
        nbits += sym_table[src[i]].nbits;
    }
    return (nbits + 7) / 8;
}

// hpack_huffman_encode compresses src bytes into dest using canonical static Huffman code table.
// Returns the number of bytes written to dest.
uint64_t hpack_huffman_encode(uint8_t *dest, const uint8_t *src, uint64_t srclen, const huffman_sym *sym_table) {
    uint8_t *p = dest;
    const uint8_t *end = src + srclen;
    uint64_t code = 0;
    uint64_t nbits = 0;

    for (; src != end; ++src) {
        const huffman_sym *sym = &sym_table[*src];
        code |= ((uint64_t)sym->code) << (32 - nbits);
        nbits += sym->nbits;
        while (nbits >= 32) {
            uint32_t x = (uint32_t)(code >> 32);
            #if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
            x = __builtin_bswap32(x);
            #endif
            __builtin_memcpy(p, &x, 4);
            p += 4;
            code <<= 32;
            nbits -= 32;
        }
    }

    while (nbits >= 8) {
        *p++ = (uint8_t)(code >> 56);
        code <<= 8;
        nbits -= 8;
    }

    if (nbits > 0) {
        *p++ = (uint8_t)((uint8_t)(code >> 56) | ((1 << (8 - nbits)) - 1));
    }

    return (uint64_t)(p - dest);
}

// hpack_huffman_decode decompresses HPACK/QPACK Huffman encoded src bytes into dest.
// Returns number of decoded bytes written to dest, or -1 on invalid padding / decode failure.
int64_t hpack_huffman_decode(uint8_t *dest, const uint8_t *src, uint64_t srclen, const huffman_decode_node *decode_table) {
    uint8_t *p = dest;
    const uint8_t *end = src + srclen;
    uint16_t fstate = 0;
    uint8_t flags = 0x01; // 0x01 = ACCEPTED

    for (; src != end; ++src) {
        uint8_t c = *src;

        uint64_t idx1 = (((uint64_t)fstate) << 4) | (uint64_t)(c >> 4);
        const huffman_decode_node *node1 = &decode_table[idx1];
        if (node1->flags & 0x02) { // SYM
            *p++ = node1->sym;
        }
        fstate = node1->fstate;

        uint64_t idx2 = (((uint64_t)fstate) << 4) | (uint64_t)(c & 0x0F);
        const huffman_decode_node *node2 = &decode_table[idx2];
        if (node2->flags & 0x02) { // SYM
            *p++ = node2->sym;
        }
        fstate = node2->fstate;
        flags = node2->flags;
    }

    if (!(flags & 0x01)) { // ACCEPTED
        return -1;
    }

    return (int64_t)(p - dest);
}
