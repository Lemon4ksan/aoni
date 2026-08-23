// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package spec provides the programmatic Abstract Syntax Tree (AST)
// and schema definitions for declaring Vortex Universal Browser Attestation Oracles.
package spec

// InterceptSource identifies where the attestation token/signature is extracted from.
type InterceptSource string

const (
	SourceRequestBody    InterceptSource = "request_body"
	SourceResponseBody   InterceptSource = "response_body"
	SourceHeader         InterceptSource = "header"
	SourceCookie         InterceptSource = "cookie"
	SourceLocalStorage   InterceptSource = "local_storage"
	SourceSessionStorage InterceptSource = "session_storage"
	SourceGlobalJS       InterceptSource = "global_js"
	SourceDOMAttr        InterceptSource = "dom_attr"
)

// StepAction identifies the browser interaction action to perform.
type StepAction string

const (
	ActionClick       StepAction = "click"
	ActionType        StepAction = "type"
	ActionSelect      StepAction = "select"
	ActionWaitVisible StepAction = "wait_visible"
	ActionWaitHidden  StepAction = "wait_hidden"
	ActionHotkey      StepAction = "hotkey"
	ActionEval        StepAction = "eval"
	ActionDelay       StepAction = "delay"
)

// OracleSpec defines the full specification of a browser attestation oracle sidecar.
type OracleSpec struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Port        int           `json:"port"`
	TargetURL   string        `json:"target_url"`
	Browser     BrowserConfig `json:"browser"`
	Flows       []FlowSpec    `json:"flows"`
}

// BrowserConfig configures the browser lifecycle, environment discovery, proxy, and concurrency pool.
type BrowserConfig struct {
	Headless         bool     `json:"headless"`
	AutoDetect       bool     `json:"auto_detect"`
	Proxy            string   `json:"proxy,omitempty"`     // e.g. "socks5://127.0.0.1:1080" or "http://..."
	PoolSize         int      `json:"pool_size,omitempty"` // Number of concurrent isolated browser tabs (default: 3)
	UserDataDir      string   `json:"user_data_dir,omitempty"`
	DismissSelectors []string `json:"dismiss_selectors,omitempty"`
	ExtraFlags       []string `json:"extra_flags,omitempty"`
}

// FlowStep defines a discrete atomic interaction step in an action sequence.
type FlowStep struct {
	Action   StepAction `json:"action"` // "click", "type", "wait_visible", "eval", etc.
	Selector string     `json:"selector,omitempty"`
	Value    string     `json:"value,omitempty"` // Supports dynamic "{content}" placeholder
	Kinetics bool       `json:"kinetics,omitempty"`
	Timeout  int        `json:"timeout_ms,omitempty"`
}

// FlowSpec defines a named kinetic interaction flow to trigger and capture tokens.
type FlowSpec struct {
	Name             string        `json:"name"`
	Steps            []FlowStep    `json:"steps,omitempty"` // Composite action sequence
	InputSelector    string        `json:"input_selector,omitempty"`
	SubmitSelector   string        `json:"submit_selector,omitempty"`
	FallbackShortcut string        `json:"fallback_shortcut,omitempty"`
	HumanKinetics    bool          `json:"human_kinetics,omitempty"`
	TimeoutMs        int           `json:"timeout_ms,omitempty"`
	Intercept        InterceptRule `json:"intercept"`
}

// InterceptRule defines what network traffic or browser state to capture from the browser runtime.
type InterceptRule struct {
	Source         InterceptSource `json:"source,omitempty"` // Defaults to "request_body"
	URLPattern     string          `json:"url_pattern,omitempty"`
	Key            string          `json:"key,omitempty"`         // Header name, LocalStorage key, or window.* expression
	Selector       string          `json:"selector,omitempty"`    // DOM selector
	Attr           string          `json:"attr,omitempty"`        // DOM attribute name (e.g. "value", "data-csrf")
	TokenIndex     int             `json:"token_index,omitempty"` // Index for positional JSON arrays
	JSONPath       string          `json:"json_path,omitempty"`   // e.g. "data.session.token"
	CaptureHeaders []string        `json:"capture_headers,omitempty"`
	CaptureCookies bool            `json:"capture_cookies"`
}
