// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mod provides declarative request modifiers for customizing an [aoni.Request] prior to execution.
package mod

import (
	"errors"

	"github.com/lemon4ksan/aoni"
)

// ErrInvalidPairCount is returned when WithVars receives an odd number of key-value arguments.
var ErrInvalidPairCount = errors.New("aoni/mod: WithVars requires an even number of key-value pairs")

// Apply executes a slice of [aoni.RequestModifier] options sequentially on req.
// Nil modifiers in mods are ignored safely without allocation.
func Apply(req aoni.Request, mods ...aoni.RequestModifier) {
	for _, m := range mods {
		if m != nil {
			m(req)
		}
	}
}

// WithAutoDecode constructs an [aoni.RequestModifier] enabling content-type header detection for response parsing.
func WithAutoDecode() aoni.RequestModifier {
	return func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).AutoDecode = true
	}
}
