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

func (s *SlogAdapter) Debug(msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.Debug(msg, args...)
}

func (s *SlogAdapter) DebugContext(ctx context.Context, msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.DebugContext(ctx, msg, args...)
}

func (s *SlogAdapter) Info(msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.Info(msg, args...)
}

func (s *SlogAdapter) InfoContext(ctx context.Context, msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.InfoContext(ctx, msg, args...)
}

func (s *SlogAdapter) Warn(msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.Warn(msg, args...)
}

func (s *SlogAdapter) WarnContext(ctx context.Context, msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.WarnContext(ctx, msg, args...)
}

func (s *SlogAdapter) Error(msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.Error(msg, args...)
}

func (s *SlogAdapter) ErrorContext(ctx context.Context, msg string, args ...any) {
	if s == nil || s.logger == nil {
		return
	}

	s.logger.ErrorContext(ctx, msg, args...)
}

// StructuredAdapter provides zero-dependency compatibility for Zap (zap.SugaredLogger / zap.Logger)
// and Zerolog (zerolog.Logger) without adding external go.mod module dependencies.
type StructuredAdapter struct {
	logFunc func(level LogLevel, msg string, keysAndValues ...any)
}

// NewStructuredAdapter creates a zero-dependency [Logger] using a custom dispatch function.
func NewStructuredAdapter(logFunc func(level LogLevel, msg string, keysAndValues ...any)) *StructuredAdapter {
	return &StructuredAdapter{logFunc: logFunc}
}

func (a *StructuredAdapter) Log(ctx context.Context, level LogLevel, msg string, keysAndValues ...any) {
	if a == nil || a.logFunc == nil {
		return
	}

	a.logFunc(level, msg, keysAndValues...)
}

func (a *StructuredAdapter) Debug(msg string, args ...any) {
	a.Log(context.Background(), LevelDebug, msg, args...)
}

func (a *StructuredAdapter) DebugContext(ctx context.Context, msg string, args ...any) {
	a.Log(ctx, LevelDebug, msg, args...)
}

func (a *StructuredAdapter) Info(msg string, args ...any) {
	a.Log(context.Background(), LevelInfo, msg, args...)
}

func (a *StructuredAdapter) InfoContext(ctx context.Context, msg string, args ...any) {
	a.Log(ctx, LevelInfo, msg, args...)
}

func (a *StructuredAdapter) Warn(msg string, args ...any) {
	a.Log(context.Background(), LevelWarn, msg, args...)
}

func (a *StructuredAdapter) WarnContext(ctx context.Context, msg string, args ...any) {
	a.Log(ctx, LevelWarn, msg, args...)
}

func (a *StructuredAdapter) Error(msg string, args ...any) {
	a.Log(context.Background(), LevelError, msg, args...)
}

func (a *StructuredAdapter) ErrorContext(ctx context.Context, msg string, args ...any) {
	a.Log(ctx, LevelError, msg, args...)
}
