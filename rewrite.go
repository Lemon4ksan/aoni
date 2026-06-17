// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
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
