// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qpack

import "golang.org/x/net/http2/hpack"

func appendHuffman(dst []byte, s string) []byte {
	return hpack.AppendHuffmanString(dst, s)
}

func huffmanLen(s string) int {
	return int(hpack.HuffmanEncodeLength(s))
}

func decodeHuffman(src []byte) (string, error) {
	return hpack.HuffmanDecodeToString(src)
}
