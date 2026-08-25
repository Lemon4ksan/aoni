// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package tun

import (
	"errors"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestCStringToGoString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{"null terminated", []byte{'t', 'u', 'n', '0', 0, 0, 0}, "tun0"},
		{"exact size no null", []byte{'t', 'u', 'n', '1'}, "tun1"},
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

func TestLinuxTunAdapter_ErrorsAndPermissions(t *testing.T) {
	t.Parallel()

	adapter, err := NewLinuxAdapter("tun99")
	if err != nil {
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrLinuxTunOpenFailed) || errors.Is(err, ErrLinuxIoctlFailed))
	} else {
		require.NotNil(t, adapter)
		assert.Equal(t, "tun99", adapter.Name())
		_ = adapter.Close()
	}
}
