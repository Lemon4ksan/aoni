// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build openbsd

package quic

// The OpenBSD kernel rejects socket buffer sizes larger than SB_MAX (2 MB, see sys/sys/socketvar.h)
// instead of clamping them. This limit is not queryable at runtime; changing it requires a custom
// kernel (sb_max is a patchable kernel variable, not a sysctl).
const desiredBufferSize = 2 << 20 // 2 MiB
