// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"maps"
	"net/http"
)

// HostRewriteConfig holds the configuration for host rewrite.
type HostRewriteConfig struct {
	Rules map[string]string
}

// WithHostRewrite returns a RequestModifier that rewrites the host header based on the provided rules.
func WithHostRewrite(rules map[string]string) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).HostRewrite = &HostRewriteConfig{Rules: rules}
	}
}

// AppendHostRewrite returns a RequestModifier that appends new host rewrite rules to the existing
// HostRewriteConfig in the request context, or creates a new one if none are present.
func AppendHostRewrite(rules map[string]string) RequestModifier {
	return func(req *http.Request) {
		cfg := getOrInitRequestConfig(req)

		newRules := make(map[string]string)
		if cfg.HostRewrite != nil && cfg.HostRewrite.Rules != nil {
			maps.Copy(newRules, cfg.HostRewrite.Rules)
		}

		maps.Copy(newRules, rules)

		cfg.HostRewrite = &HostRewriteConfig{Rules: newRules}
	}
}

// HostRewriteRules extracts and returns the active host rewrite rules map from the given context.
// Returns nil if no rules are configured in the context.
func HostRewriteRules(ctx context.Context) map[string]string {
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.HostRewrite != nil {
		return cfg.HostRewrite.Rules
	}

	return nil
}
