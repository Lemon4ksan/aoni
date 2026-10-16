// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
)

func TestClient_Close_RunsOnCloseHooksOnce(t *testing.T) {
	t.Parallel()

	var order []int

	client := NewClient(nil, func(cfg *Config) {
		cfg.Ext.OnClose = append(cfg.Ext.OnClose,
			func() { order = append(order, 1) },
			nil, // nil hooks must be ignored
			func() { order = append(order, 2) },
		)
	})

	client.Close()
	client.Close() // repeated Close must not re-run hooks

	assert.Equal(t, []int{1, 2}, order)
}

func TestConfig_Clone_IsolatesOnCloseHooks(t *testing.T) {
	t.Parallel()

	orig := Config{}
	orig.Ext.OnClose = []func(){func() {}}

	cloned := orig.Clone()
	cloned.Ext.OnClose = append(cloned.Ext.OnClose, func() {})

	assert.Equal(t, 1, len(orig.Ext.OnClose))
	assert.Equal(t, 2, len(cloned.Ext.OnClose))
}
