// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"maps"
	"net/http"
)

type hostRewriteCtxKey struct{}

// HostRewriteConfig holds the configuration for host rewrite.
type HostRewriteConfig struct {
	Rules map[string]string
}

// WithHostRewrite returns a RequestModifier that rewrites the host header based on the provided rules.
func WithHostRewrite(rules map[string]string) RequestModifier {
	return func(req *http.Request) {
		cfg := &HostRewriteConfig{Rules: rules}
		ctx := context.WithValue(req.Context(), hostRewriteCtxKey{}, cfg)
		*req = *req.WithContext(ctx)
	}
}

// AppendHostRewrite returns a RequestModifier that appends new host rewrite rules to the existing
// HostRewriteConfig in the request context, or creates a new one if none are present.
func AppendHostRewrite(rules map[string]string) RequestModifier {
	return func(req *http.Request) {
		var existing *HostRewriteConfig
		if val, ok := req.Context().Value(hostRewriteCtxKey{}).(*HostRewriteConfig); ok && val != nil {
			existing = val
		}

		newRules := make(map[string]string)
		if existing != nil && existing.Rules != nil {
			maps.Copy(newRules, existing.Rules)
		}

		maps.Copy(newRules, rules)

		cfg := &HostRewriteConfig{Rules: newRules}
		ctx := context.WithValue(req.Context(), hostRewriteCtxKey{}, cfg)
		*req = *req.WithContext(ctx)
	}
}

// HostRewriteRules extracts and returns the active host rewrite rules map from the given context.
// Returns nil if no rules are configured in the context.
func HostRewriteRules(ctx context.Context) map[string]string {
	if cfg, ok := ctx.Value(hostRewriteCtxKey{}).(*HostRewriteConfig); ok && cfg != nil {
		return cfg.Rules
	}

	return nil
}
