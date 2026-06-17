// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"testing"

	"github.com/lemon4ksan/aoni"
)

func TestWithHostRewrite(t *testing.T) {
	rules := map[string]string{
		"example.com": "1.2.3.4:443",
	}

	mod := aoni.WithHostRewrite(rules)
	if mod == nil {
		t.Error("WithHostRewrite returned nil")
	}
}

func TestWithHostRewriteEmptyRules(t *testing.T) {
	mod := aoni.WithHostRewrite(map[string]string{})
	if mod == nil {
		t.Error("WithHostRewrite returned nil for empty rules")
	}
}

func TestHostRewriteConfig(t *testing.T) {
	cfg := &aoni.HostRewriteConfig{
		Rules: map[string]string{
			"example.com": "1.2.3.4:443",
			"test.org":    "5.6.7.8:8443",
		},
	}

	if len(cfg.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(cfg.Rules))
	}

	if cfg.Rules["example.com"] != "1.2.3.4:443" {
		t.Errorf("unexpected rule for example.com: %s", cfg.Rules["example.com"])
	}
}
