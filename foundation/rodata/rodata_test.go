// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rodata_test

import (
	"bytes"
	"testing"

	"github.com/lemon4ksan/aoni/foundation/rodata"
)

func TestInternKey(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
	}{
		{":method", rodata.PseudoMethod},
		{":authority", rodata.PseudoAuthority},
		{":scheme", rodata.PseudoScheme},
		{":path", rodata.PseudoPath},
		{":status", rodata.PseudoStatus},
		{"content-type", rodata.KeyContentType},
		{"Content-Type", rodata.KeyContentType},
		{"ACCEPT-ENCODING", rodata.KeyAcceptEncoding},
		{"user-agent", rodata.KeyUserAgent},
		{"sec-ch-ua", rodata.KeySecChUa},
		{"unknown-header", nil},
		{"", nil},
	}

	for _, tt := range tests {
		got := rodata.InternKey(tt.input)
		if tt.expected == nil {
			if got != nil {
				t.Errorf("InternKey(%q) = %s, want nil", tt.input, got)
			}
		} else {
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("InternKey(%q) = %s, want %s", tt.input, got, tt.expected)
			}
		}
	}
}

func TestInternValue(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
	}{
		{"application/json", rodata.ValApplicationJSON},
		{"gzip, deflate, br, zstd", rodata.ValAcceptEncodingGzip},
		{"keep-alive", rodata.ValConnectionKeepAlive},
		{"unknown-val", nil},
	}

	for _, tt := range tests {
		got := rodata.InternValue(tt.input)
		if tt.expected == nil {
			if got != nil {
				t.Errorf("InternValue(%q) = %s, want nil", tt.input, got)
			}
		} else {
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("InternValue(%q) = %s, want %s", tt.input, got, tt.expected)
			}
		}
	}
}
