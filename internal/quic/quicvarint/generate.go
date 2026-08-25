// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quicvarint

//go:generate c2plan9 -c ../../../csrc/quic_varint.c -o varint_amd64.s -stub varint_amd64.go -pkg quicvarint
