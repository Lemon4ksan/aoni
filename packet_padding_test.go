// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaddingHeaderName(t *testing.T) {
	t.Parallel()

	t.Run("default_when_empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "X-Padding", PaddingHeaderName(PacketPaddingConfig{}))
	})

	t.Run("uses_custom_header", func(t *testing.T) {
		t.Parallel()

		cfg := PacketPaddingConfig{PaddingHeader: "X-Custom"}
		assert.Equal(t, "X-Custom", PaddingHeaderName(cfg))
	})

	t.Run("header_pool_overrides_custom", func(t *testing.T) {
		t.Parallel()

		cfg := PacketPaddingConfig{
			PaddingHeader: "X-ShouldBeIgnored",
			HeaderPool:    []string{"X-Amz-Trace-Id", "CF-RAY", "X-Request-ID"},
		}

		seen := make(map[string]bool)
		for range 100 {
			name := PaddingHeaderName(cfg)
			seen[name] = true
		}

		assert.Equal(t, 3, len(seen), "all pool entries should be selected")
	})

	t.Run("single_entry_pool", func(t *testing.T) {
		t.Parallel()

		cfg := PacketPaddingConfig{HeaderPool: []string{"CF-RAY"}}
		for range 50 {
			assert.Equal(t, "CF-RAY", PaddingHeaderName(cfg))
		}
	})
}

func TestGeneratePadding(t *testing.T) {
	t.Parallel()

	t.Run("returns_nil_when_disabled", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, GeneratePadding(PacketPaddingConfig{}))
	})

	t.Run("returns_bytes_in_range", func(t *testing.T) {
		t.Parallel()

		cfg := PacketPaddingConfig{MinPaddingBytes: 10, MaxPaddingBytes: 20}
		for range 50 {
			padding := GeneratePadding(cfg)
			assert.GreaterOrEqual(t, len(padding), 10)
			assert.LessOrEqual(t, len(padding), 20)
		}
	})

	t.Run("min_eq_max", func(t *testing.T) {
		t.Parallel()

		cfg := PacketPaddingConfig{MinPaddingBytes: 8, MaxPaddingBytes: 8}
		padding := GeneratePadding(cfg)
		assert.Len(t, padding, 8)
	})
}
