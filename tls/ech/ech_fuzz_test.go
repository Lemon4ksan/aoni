// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ech_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/tls/ech"
)

func FuzzParseECHConfigList(f *testing.F) {
	f.Add(
		[]byte(
			"\x00\x1e\xfe\x0d\x00\x1a\x00\x01\x00\x20\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00",
		),
	)
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, data []byte) {
		list, err := ech.ParseConfigList(data)
		if err == nil && len(list) > 0 {
			encoded, err2 := ech.MarshalConfigList(list)
			if err2 == nil && len(encoded) > 0 {
				_, _ = ech.ParseConfigList(encoded)
			}
		}
	})
}

func FuzzParseECHConfigListBase64(f *testing.F) {
	f.Add("AE7+DQAfAQAgABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	f.Add("")
	f.Add("invalid-base64-payload!!!")

	f.Fuzz(func(t *testing.T, rawB64 string) {
		_, _ = ech.ParseBase64(rawB64)
	})
}
