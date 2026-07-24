// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bytesconv

import (
	"bytes"
	"testing"
)

func TestB2SAndS2B(t *testing.T) {
	if s := B2S(nil); s != "" {
		t.Errorf("B2S(nil) = %q, want empty", s)
	}
	if b := S2B(""); b != nil {
		t.Errorf("S2B(\"\") = %v, want nil", b)
	}

	str := "Hello, Aoni!"
	b := S2B(str)
	if !bytes.Equal(b, []byte(str)) {
		t.Errorf("S2B(%q) = %v, want %v", str, b, []byte(str))
	}

	s := B2S(b)
	if s != str {
		t.Errorf("B2S(%v) = %q, want %q", b, s, str)
	}
}

func TestLowercaseByte(t *testing.T) {
	tests := []struct {
		in   byte
		want byte
	}{
		{'A', 'a'},
		{'Z', 'z'},
		{'a', 'a'},
		{'z', 'z'},
		{'0', '0'},
		{'-', '-'},
	}

	for _, tt := range tests {
		if got := LowercaseByte(tt.in); got != tt.want {
			t.Errorf("LowercaseByte(%c) = %c, want %c", tt.in, got, tt.want)
		}
	}
}

func TestEqualFoldASCII(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"Content-Type", "content-type", true},
		{"USER-AGENT", "user-agent", true},
		{"Hello", "World", false},
		{"Short", "LongerString", false},
	}

	for _, tt := range tests {
		if got := EqualFoldASCII(tt.a, tt.b); got != tt.want {
			t.Errorf("EqualFoldASCII(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestAppendToLower(t *testing.T) {
	if got := AppendToLower(nil, nil); len(got) != 0 {
		t.Errorf("AppendToLower(nil, nil) = %v, want empty", got)
	}

	dst := []byte("Header: ")
	src := []byte("X-Aoni-Test")
	got := AppendToLower(dst, src)
	want := "Header: x-aoni-test"
	if string(got) != want {
		t.Errorf("AppendToLower result = %q, want %q", string(got), want)
	}
}

func TestTrimQuotes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`"hello"`, "hello"},
		{`"a"`, "a"},
		{`""`, ""},
		{`noquotes`, "noquotes"},
		{`"unmatched`, `"unmatched`},
	}

	for _, tt := range tests {
		got := TrimQuotes([]byte(tt.in))
		if string(got) != tt.want {
			t.Errorf("TrimQuotes(%q) = %q, want %q", tt.in, string(got), tt.want)
		}
	}
}
