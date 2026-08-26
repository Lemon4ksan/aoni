// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ech_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/fingerprint/ech"
)

func FuzzParseECHConfigList(f *testing.F) {
	f.Add([]byte{0xfe, 0x0d, 0x00, 0x14, 0x00, 0x20, 0x00, 0x01, 0x00, 0x01, 0x00, 0x01, 0x00, 0x01})
	f.Add([]byte{})
	f.Add([]byte{0xfe, 0x0d})

	f.Fuzz(func(t *testing.T, data []byte) {
		configs, err := ech.ParseConfigList(data)
		if err == nil && len(configs) > 0 {
			marshaled, mErr := ech.MarshalConfigList(configs)
			if mErr == nil && len(marshaled) == 0 {
				t.Fatalf("expected non-empty marshaled config list")
			}
		}

		_, _, _ = ech.ParseConfig(data)
	})
}

func FuzzParseECHBase64(f *testing.F) {
	f.Add("AEb+DQBEFwAgACD6r1XgLhXkG5g1JjH3R7PqT8W4b6l0jH1Z2x3Y4A==")
	f.Add("")
	f.Add("not_valid_base64!@#$%^&*")

	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = ech.ParseBase64(raw)
		_ = ech.ValidatePublicName(raw)
	})
}
