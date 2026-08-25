// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin

package tun

import (
	"errors"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestDarwinCStringToGoString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{"null terminated", []byte{'u', 't', 'u', 'n', '0', 0, 0}, "utun0"},
		{"exact size no null", []byte{'u', 't', 'u', 'n', '1'}, "utun1"},
		{"empty byte slice", []byte{}, ""},
		{"leading null", []byte{0, 'a', 'b'}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cStringToGoString(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNewDarwinTunAdapter_InvalidName(t *testing.T) {
	t.Parallel()

	invalidNames := []string{
		"tun0",
		"eth0",
		"utunABC",
		"utun-1",
		"invalid_utun",
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewDarwinAdapter(name)
			assert.ErrorIs(t, err, ErrInvalidUtunName, "name %s should return ErrInvalidUtunName", name)
		})
	}
}
