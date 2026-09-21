// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mod provides declarative request modifiers for customizing an [aoni.Request] prior to execution.
package mod

import (
	"errors"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/pipeline"
)

type (
	// Request is an alias for core.Request.
	Request = core.Request

	// RequestModifier is an alias for core.RequestModifier.
	RequestModifier = core.RequestModifier

	// RetryOverride is an alias for core.RetryOverride.
	RetryOverride = core.RetryOverride

	// FallbackFunc is an alias for core.FallbackFunc.
	FallbackFunc = core.FallbackFunc

	// SoftErrorDetector is an alias for core.SoftErrorDetector.
	SoftErrorDetector = core.SoftErrorDetector

	// ResponseDecoder is an alias for core.ResponseDecoder.
	ResponseDecoder = core.ResponseDecoder

	// ProgressFunc is an alias for core.ProgressFunc.
	ProgressFunc = core.ProgressFunc
)

var getOrInitRequestConfig = pipeline.GetOrInitRequestConfig

// ErrInvalidPairCount is returned when WithVars receives an odd number of key-value arguments.
var ErrInvalidPairCount = errors.New("aoni/mod: WithVars requires an even number of key-value pairs")

// Apply executes a slice of [RequestModifier] options sequentially on req.
func Apply(req Request, mods ...RequestModifier) {
	for _, m := range mods {
		m.Apply(req)
	}
}

// WithAutoDecode instructs the client engine to detect the response Content-Type and select the optimal decoder.
func WithAutoDecode() RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			getOrInitRequestConfig(req).AutoDecode = true
		},
	}
}

// Custom constructs a custom [RequestModifier] wrapping an arbitrary closure function.
//
// # Example
//
//	resp, err := client.Get(ctx, "/resource",
//	    mod.Custom(func(req aoni.Request) {
//	        req.SetHeader("X-Custom", "value")
//	    }),
//	)
func Custom(fn func(Request)) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn:   fn,
	}
}
