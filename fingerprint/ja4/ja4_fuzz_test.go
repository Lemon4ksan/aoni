// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ja4_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
)

// FuzzParseExtensionsFromRaw tests ClientHello binary extension and signature algorithm parsing.
func FuzzParseExtensionsFromRaw(f *testing.F) {
	f.Add([]byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x01, 0x00})
	f.Add([]byte{})
	f.Add([]byte{0x16, 0x03, 0x03})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		exts, sigs := ja4.ParseExtensionsFromRaw(data)
		_ = exts
		_ = sigs
	})
}

// FuzzComputeJA4 tests JA4 fingerprint formatting across arbitrary cipher suites and extensions.
func FuzzComputeJA4(f *testing.F) {
	f.Add(uint16(0x1301), uint16(0x0000), uint16(0x0304), true, "h2", uint16(0x0403))
	f.Add(uint16(0x002f), uint16(0x0010), uint16(0x0303), false, "http/1.1", uint16(0x0804))
	f.Add(uint16(0), uint16(0), uint16(0), false, "", uint16(0))

	f.Fuzz(func(t *testing.T, cipher, ext, ver uint16, sni bool, alpn string, sig uint16) {
		ciphers := []uint16{cipher}
		exts := []uint16{ext}
		vers := []uint16{ver}
		alpns := []string{alpn}
		sigs := []uint16{sig}

		res := ja4.ComputeJA4(ciphers, exts, vers, sni, alpns, sigs)
		if len(res) < 10 {
			t.Fatalf("unexpected short JA4 fingerprint: %s", res)
		}
	})
}

// FuzzComputeJA4H tests JA4H HTTP client fingerprinting against arbitrary method, headers, and cookies.
func FuzzComputeJA4H(f *testing.F) {
	f.Add("GET", "HTTP/2", "Accept-Language", true, true, "en-US", "sid", "secret")
	f.Add("POST", "HTTP/1.1", "Content-Type", false, false, "ru-RU", "", "")
	f.Add("", "", "", false, false, "", "", "")

	f.Fuzz(
		func(t *testing.T, method, proto, header string, hasCookie, hasReferer bool, lang, cookieName, cookieVal string) {
			headers := []string{header}
			cookieNames := []string{cookieName}
			cookieVals := []string{cookieVal}

			res := ja4.ComputeJA4H(method, proto, headers, hasCookie, hasReferer, lang, cookieNames, cookieVals)
			if len(res) < 10 {
				t.Fatalf("unexpected short JA4H fingerprint: %s", res)
			}
		},
	)
}
