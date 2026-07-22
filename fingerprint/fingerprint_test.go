// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fingerprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePadding(t *testing.T) {
	t.Parallel()

	t.Run("returns_nil_when_disabled", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, GeneratePadding(PaddingConfig{}))
	})

	t.Run("returns_bytes_in_range", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{MinPaddingBytes: 10, MaxPaddingBytes: 20}
		for range 50 {
			padding := GeneratePadding(cfg)
			assert.GreaterOrEqual(t, len(padding), 10)
			assert.LessOrEqual(t, len(padding), 20)
		}
	})

	t.Run("min_eq_max", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{MinPaddingBytes: 8, MaxPaddingBytes: 8}
		padding := GeneratePadding(cfg)
		assert.Len(t, padding, 8)
	})

	t.Run("min_and_max_boundary_adjustments", func(t *testing.T) {
		t.Parallel()

		// Min is negative -> should default to 1
		cfg1 := PaddingConfig{MinPaddingBytes: -5, MaxPaddingBytes: 10}
		padding1 := GeneratePadding(cfg1)
		assert.GreaterOrEqual(t, len(padding1), 1)

		// Max is less than min -> should default max to min
		cfg2 := PaddingConfig{MinPaddingBytes: 5, MaxPaddingBytes: 2}
		padding2 := GeneratePadding(cfg2)
		assert.Len(t, padding2, 5)
	})
}

func TestGeneratePadding_SafetyAndHeaderPools(t *testing.T) {
	t.Parallel()

	t.Run("negative_padding_ranges", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{
			MinPaddingBytes: -20,
			MaxPaddingBytes: -10,
		}
		assert.Nil(t, GeneratePadding(cfg))
	})

	t.Run("inverted_padding_ranges", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{
			MinPaddingBytes: 30,
			MaxPaddingBytes: 10,
		}
		res := GeneratePadding(cfg)
		require.NotNil(t, res)
		assert.Len(t, res, 30, "inverted range should align size to min boundary")
	})

	t.Run("header_pool_selection", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{
			HeaderPool: []string{"X-Test-1", "X-Test-2"},
		}

		header := PaddingHeaderName(cfg)
		assert.Contains(t, cfg.HeaderPool, header)

		defaultCfg := PaddingConfig{}
		assert.Equal(t, "X-Padding", PaddingHeaderName(defaultCfg))
	})
}

func TestPaddingHeaderName(t *testing.T) {
	t.Parallel()

	t.Run("default_when_empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "X-Padding", PaddingHeaderName(PaddingConfig{}))
	})

	t.Run("uses_custom_header", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{PaddingHeader: "X-Custom"}
		assert.Equal(t, "X-Custom", PaddingHeaderName(cfg))
	})

	t.Run("header_pool_overrides_custom", func(t *testing.T) {
		t.Parallel()

		cfg := PaddingConfig{
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

		cfg := PaddingConfig{HeaderPool: []string{"CF-RAY"}}
		for range 50 {
			assert.Equal(t, "CF-RAY", PaddingHeaderName(cfg))
		}
	})
}
