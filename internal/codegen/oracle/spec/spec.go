// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package spec provides the programmatic Abstract Syntax Tree (AST)
// and schema definitions for declaring Vortex Browser Attestation Oracles.
package spec

// OracleSpec defines the full specification of a browser attestation oracle sidecar.
type OracleSpec struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Port        int           `json:"port"`
	TargetURL   string        `json:"target_url"`
	Browser     BrowserConfig `json:"browser"`
	Flows       []FlowSpec    `json:"flows"`
}

// BrowserConfig configures the browser lifecycle and environment discovery.
type BrowserConfig struct {
	Headless         bool     `json:"headless"`
	AutoDetect       bool     `json:"auto_detect"`
	UserDataDir      string   `json:"user_data_dir,omitempty"`
	DismissSelectors []string `json:"dismiss_selectors,omitempty"`
	ExtraFlags       []string `json:"extra_flags,omitempty"`
}

// FlowSpec defines a named kinetic interaction flow to trigger and capture tokens.
type FlowSpec struct {
	Name             string        `json:"name"`
	InputSelector    string        `json:"input_selector,omitempty"`
	SubmitSelector   string        `json:"submit_selector,omitempty"`
	FallbackShortcut string        `json:"fallback_shortcut,omitempty"`
	HumanKinetics    bool          `json:"human_kinetics"`
	TimeoutMs        int           `json:"timeout_ms,omitempty"`
	Intercept        InterceptRule `json:"intercept"`
}

// InterceptRule defines what network traffic to capture from the browser runtime.
type InterceptRule struct {
	URLPattern     string   `json:"url_pattern"`
	ExtractPath    string   `json:"extract_path,omitempty"` // e.g. "body[4]" or "header.Authorization"
	TokenIndex     int      `json:"token_index,omitempty"`  // index in positional tuple
	CaptureHeaders []string `json:"capture_headers,omitempty"`
	CaptureCookies bool     `json:"capture_cookies"`
}
