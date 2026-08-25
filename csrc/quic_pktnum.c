// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include <stdint.h>
#include <stddef.h>

// quic_decode_packet_number calculates the full 62-bit packet number from truncated wire bytes (RFC 9000 Appendix A.3).
// Pure register-based 64-bit window math.
int64_t quic_decode_packet_number(uint64_t length, int64_t largest, int64_t truncated) {
    int64_t expected = largest + 1;
    int64_t win = 1LL << (length * 8);
    int64_t hwin = win >> 1;
    int64_t mask = win - 1;

    int64_t candidate = (expected & ~mask) | truncated;
    if (candidate <= expected - hwin && candidate < (1LL << 62) - win) {
        return candidate + win;
    }
    if (candidate > expected + hwin && candidate >= win) {
        return candidate - win;
    }
    return candidate;
}

// quic_packet_number_len_for_header determines the packet number length for header encoding (RFC 9000 §17.1).
// Returns 2, 3, or 4 bytes.
uint64_t quic_packet_number_len_for_header(int64_t pn, int64_t largest_acked) {
    int64_t num_unacked;
    if (largest_acked < 0) {
        num_unacked = pn + 1;
    } else {
        num_unacked = pn - largest_acked;
    }

    if (num_unacked < 32768LL) {
        return 2;
    }
    if (num_unacked < 8388608LL) {
        return 3;
    }
    return 4;
}
