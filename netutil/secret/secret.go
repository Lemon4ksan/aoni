// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package secret provides protected in-memory containers for sensitive authentication material,
// preventing accidental leakage in log outputs, stack traces, JSON serialization, and debug dumps.
package secret

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// Secret wraps sensitive data (passwords, bearer tokens, API keys) to prevent accidental leakage in logs and JSON dumps.
type Secret[T any] struct {
	val T
}

// New creates a new protected [Secret] container wrapping sensitive value val.
func New[T any](val T) Secret[T] {
	return Secret[T]{val: val}
}

// Value returns the raw sensitive value for authorized networking operations.
func (s Secret[T]) Value() T {
	return s.val
}

// Expose returns the raw sensitive value. Synonym for [Value].
func (s Secret[T]) Expose() T {
	return s.val
}

// String masks the secret when formatted with %s, %v, or fmt.Println.
func (s Secret[T]) String() string {
	return "******"
}

// GoString masks the secret when formatted with %#v in debug prints.
func (s Secret[T]) GoString() string {
	return "secret.Secret(******)"
}

// Format masks the secret during fmt.Sprintf printing.
func (s Secret[T]) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = f.Write([]byte("secret.Secret(******)"))
		return
	}
	_, _ = f.Write([]byte("******"))
}

// LogValue implements [slog.LogValuer] for structured logging with log/slog.
func (s Secret[T]) LogValue() slog.Value {
	return slog.StringValue("******")
}

// MarshalJSON safely serializes the secret as a masked string to prevent accidental leakage in JSON logs or responses.
func (s Secret[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal("******")
}

// UnmarshalJSON deserializes a raw value into the protected container.
func (s *Secret[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &s.val)
}
