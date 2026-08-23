// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry

import (
	"context"
	"log/slog"
)

// LogLevel specifies structured log severity.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Logger is a generic zero-dependency structured logger interface compatible with slog, zap, zerolog, and stdlib log.
type Logger interface {
	Log(ctx context.Context, level LogLevel, msg string, keysAndValues ...any)
}

// SlogAdapter adapts standard library [slog.Logger] to the zero-dependency [Logger] interface.
type SlogAdapter struct {
	logger *slog.Logger
}

// NewSlogAdapter creates a [Logger] backed by Go's native [slog.Logger].
// If l is nil, slog.Default() is used.
func NewSlogAdapter(l *slog.Logger) *SlogAdapter {
	if l == nil {
		l = slog.Default()
	}

	return &SlogAdapter{logger: l}
}

// Unwrap returns the underlying [*slog.Logger].
func (s *SlogAdapter) Unwrap() any {
	if s == nil {
		return nil
	}

	return s.logger
}

// Logger returns the underlying [*slog.Logger].
func (s *SlogAdapter) Logger() *slog.Logger {
	if s == nil {
		return nil
	}

	return s.logger
}

func (s *SlogAdapter) Log(ctx context.Context, level LogLevel, msg string, keysAndValues ...any) {
	if s == nil || s.logger == nil {
		return
	}

	var slogLevel slog.Level
	switch level {
	case LevelDebug:
		slogLevel = slog.LevelDebug
	case LevelInfo:
		slogLevel = slog.LevelInfo
	case LevelWarn:
		slogLevel = slog.LevelWarn
	case LevelError:
		slogLevel = slog.LevelError
	}

	s.logger.Log(ctx, slogLevel, msg, keysAndValues...)
}

// StructuredAdapter provides zero-dependency compatibility for Zap (zap.SugaredLogger / zap.Logger)
// and Zerolog (zerolog.Logger) without adding external go.mod module dependencies.
type StructuredAdapter struct {
	logFunc func(level LogLevel, msg string, keysAndValues ...any)
}

// NewStructuredAdapter creates a zero-dependency [Logger] using a custom dispatch function.
//
// Usage Examples:
//
// Zap SugaredLogger:
//
//	logger := telemetry.NewStructuredAdapter(func(level telemetry.LogLevel, msg string, kv ...any) {
//	    zapSugared.Infow(msg, kv...)
//	})
//
// Zerolog Logger:
//
//	logger := telemetry.NewStructuredAdapter(func(level telemetry.LogLevel, msg string, kv ...any) {
//	    zerologLogger.Info().Fields(kv).Msg(msg)
//	})
func NewStructuredAdapter(logFunc func(level LogLevel, msg string, keysAndValues ...any)) *StructuredAdapter {
	return &StructuredAdapter{logFunc: logFunc}
}

func (a *StructuredAdapter) Log(_ context.Context, level LogLevel, msg string, keysAndValues ...any) {
	if a == nil || a.logFunc == nil {
		return
	}

	a.logFunc(level, msg, keysAndValues...)
}
