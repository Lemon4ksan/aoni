// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

//go:generate c2plan9 -c ../../../../csrc/quic_pktnum.c -o pktnum_amd64.s -stub pktnum_amd64.go -pkg protocol
