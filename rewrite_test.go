// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithHostRewrite(t *testing.T) {
	t.Parallel()

	rules := map[string]string{
		"example.com": "1.2.3.4:443",
	}

	mod := WithHostRewrite(rules)
	require.NotNil(t, mod)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	mod(req)

	extracted := HostRewriteRules(req.Context())
	require.NotNil(t, extracted)
	assert.Equal(t, "1.2.3.4:443", extracted["example.com"])
}

func TestAppendHostRewrite(t *testing.T) {
	t.Parallel()

	t.Run("append_to_nil_context", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		mod := AppendHostRewrite(map[string]string{"a.com": "10.0.0.1"})
		mod(req)

		rules := HostRewriteRules(req.Context())
		require.Len(t, rules, 1)
		assert.Equal(t, "10.0.0.1", rules["a.com"])
	})

	t.Run("append_and_merge_existing_rules", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		// Apply initial rules
		WithHostRewrite(map[string]string{
			"a.com": "10.0.0.1",
			"b.com": "10.0.0.2",
		})(req)

		// Append and overwrite some rules
		AppendHostRewrite(map[string]string{
			"b.com": "10.0.0.99", // overwrite
			"c.com": "10.0.0.3",  // new
		})(req)

		rules := HostRewriteRules(req.Context())
		require.Len(t, rules, 3)
		assert.Equal(t, "10.0.0.1", rules["a.com"])
		assert.Equal(t, "10.0.0.99", rules["b.com"])
		assert.Equal(t, "10.0.0.3", rules["c.com"])
	})
}

func TestHostRewriteRules_Missing_ReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, HostRewriteRules(t.Context()))
	assert.Nil(t, HostRewriteRules(context.Background()))
}

func TestHostRewriteConfig_Direct(t *testing.T) {
	t.Parallel()

	cfg := &HostRewriteConfig{
		Rules: map[string]string{
			"example.com": "1.2.3.4:443",
			"test.org":    "5.6.7.8:8443",
		},
	}

	assert.Len(t, cfg.Rules, 2)
	assert.Equal(t, "1.2.3.4:443", cfg.Rules["example.com"])
}
