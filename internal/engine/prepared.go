// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"net/url"
	"strings"
)

// PreparedConfig holds precomputed optimized configuration values.
type PreparedConfig struct {
	BaseURL              *url.URL
	BaseURLString        string
	BaseURLTrimmedString string
}

// NewPreparedConfig constructs a PreparedConfig from a base URL.
func NewPreparedConfig(baseURL *url.URL) PreparedConfig {
	prep := PreparedConfig{BaseURL: baseURL}
	if baseURL != nil {
		prep.BaseURLString = baseURL.String()
		prep.BaseURLTrimmedString = strings.TrimSuffix(prep.BaseURLString, "/")
	}

	return prep
}
