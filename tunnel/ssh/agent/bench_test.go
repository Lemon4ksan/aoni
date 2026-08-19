// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agent_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/tunnel/ssh/agent"
)

func BenchmarkAgent(b *testing.B) {
	b.Setenv("SSH_AUTH_SOCK", "/tmp/bench-agent.sock")

	b.Run("HasAgent", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_ = agent.HasAgent()
		}
	})

	b.Run("SocketPath", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_ = agent.SocketPath()
		}
	})

	b.Run("DefaultAuthMethod", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_ = agent.DefaultAuthMethod()
		}
	})
}
