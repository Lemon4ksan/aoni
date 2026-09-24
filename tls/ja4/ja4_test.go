// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ja4_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/tls/grease"
	"github.com/lemon4ksan/aoni/tls/ja4"
	"github.com/lemon4ksan/foundation/testing/assert"
)

func TestIsGREASE(t *testing.T) {
	t.Parallel()

	greaseArr := []uint16{
		0x0a0a, 0x1a1a, 0x2a2a, 0x3a3a, 0x4a4a, 0x5a5a, 0x6a6a, 0x7a7a,
		0x8a8a, 0x9a9a, 0xaaaa, 0xbaba, 0xcaca, 0xdada, 0xeaea, 0xfafa,
	}

	for _, v := range greaseArr {
		assert.True(t, grease.Is(v), "0x%04x should be GREASE", v)
	}

	notGREASE := []uint16{0x0000, 0x0001, 0x000d, 0x0010, 0x1301, 0xc02f, 0x0303, 0x0a0b}
	for _, v := range notGREASE {
		assert.False(t, grease.Is(v), "0x%04x should not be GREASE", v)
	}
}

func TestComputeJA4_KnownVector(t *testing.T) {
	t.Parallel()

	// Example from JA4 spec: Chrome TLS 1.3 with domain SNI, 15 ciphers, 16 extensions, h2 ALPN
	ciphers := []uint16{
		0x002f, 0x0035, 0x009c, 0x009d, 0x1301, 0x1302, 0x1303,
		0xc013, 0xc014, 0xc02b, 0xc02c, 0xc02f, 0xc030, 0xcca8, 0xcca9,
	}
	extensions := []uint16{
		0x0000, 0x0005, 0x000a, 0x000b, 0x000d, 0x0010, 0x0012,
		0x0015, 0x0017, 0x001b, 0x0023, 0x002b, 0x002d, 0x0033, 0x4469, 0xff01,
	}
	supportedVersions := []uint16{0x0304} // TLS 1.3
	alpn := []string{"h2"}
	sigAlgos := []uint16{0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601}

	result := ja4.ComputeJA4(ciphers, extensions, supportedVersions, true, alpn, sigAlgos)
	assert.Regexp(t, `^t13d1516h2_[a-f0-9]{12}_[a-f0-9]{12}$`, result)
}

func TestComputeJA4H_KnownVector(t *testing.T) {
	t.Parallel()

	headers := []string{"Accept", "Accept-Encoding", "Accept-Language", "User-Agent"}
	cookieNames := []string{"session_id", "csrf_token"}
	cookieValues := []string{"abc123xyz", "tok456"}

	result := ja4.ComputeJA4H("GET", "HTTP/2", headers, true, false, "en-US,en;q=0.9", cookieNames, cookieValues)
	assert.Regexp(t, `^ge20cn04enus_[a-f0-9]{12}_[a-f0-9]{12}_[a-f0-9]{12}$`, result)
}
