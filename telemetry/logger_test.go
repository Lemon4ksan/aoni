// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/telemetry"
)

func TestSlogAdapter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	handler := slog.NewTextHandler(&buf, nil)
	slogger := slog.New(handler)

	adapter := telemetry.NewSlogAdapter(slogger)
	require.NotNil(t, adapter)

	adapter.Log(context.Background(), telemetry.LevelInfo, "test log message", "key", "value")

	output := buf.String()
	assert.Contains(t, output, "test log message")
	assert.Contains(t, output, "key=value")
}

func TestSlogAdapter_DefaultFallback(t *testing.T) {
	t.Parallel()

	adapter := telemetry.NewSlogAdapter(nil)
	require.NotNil(t, adapter)
	assert.NotPanics(t, func() {
		adapter.Log(context.Background(), telemetry.LevelDebug, "debug msg")
	})
}

func TestStructuredAdapter_ZapAndZerologSimulation(t *testing.T) {
	t.Parallel()

	var (
		loggedMsg   string
		loggedLevel telemetry.LogLevel
		loggedKV    []any
	)

	adapter := telemetry.NewStructuredAdapter(func(level telemetry.LogLevel, msg string, kv ...any) {
		loggedLevel = level
		loggedMsg = msg
		loggedKV = kv
	})

	adapter.Log(context.Background(), telemetry.LevelWarn, "warn message", "user_id", 123)

	assert.Equal(t, telemetry.LevelWarn, loggedLevel)
	assert.Equal(t, "warn message", loggedMsg)
	assert.Equal(t, []any{"user_id", 123}, loggedKV)
}
