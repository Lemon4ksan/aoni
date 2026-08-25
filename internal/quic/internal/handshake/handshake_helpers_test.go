// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handshake

import (
	"crypto/fips140"
	"crypto/tls"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

func splitHexString(t *testing.T, s string) (slice []byte) {
	t.Helper()

	for ss := range strings.SplitSeq(s, " ") {
		if ss[0:2] == "0x" {
			ss = ss[2:]
		}

		d, err := hex.DecodeString(ss)
		require.NoError(t, err)

		slice = append(slice, d...)
	}

	return slice
}

func TestSplitHexString(t *testing.T) {
	require.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, splitHexString(t, "0xdeadbeef"))
	require.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, splitHexString(t, "deadbeef"))
	require.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, splitHexString(t, "dead beef"))
}

var cipherSuites = []cipherSuite{
	getCipherSuite(tls.TLS_AES_128_GCM_SHA256),
	getCipherSuite(tls.TLS_AES_256_GCM_SHA384),
}

func init() {
	if !fips140.Enabled() {
		cipherSuites = append(cipherSuites, getCipherSuite(tls.TLS_CHACHA20_POLY1305_SHA256))
	}
}
