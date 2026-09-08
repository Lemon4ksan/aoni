// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tarpit_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/tunnel/ssh/tarpit"
)

func TestTrap(t *testing.T) {
	t.Parallel()

	t.Run("canceled_context_immediate", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		serverConn, clientConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			tarpit.Trap(ctx, serverConn, 100*time.Millisecond)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Trap did not exit promptly on canceled context")
		}
	})

	t.Run("banner_generation_and_streaming", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		serverConn, clientConn := net.Pipe()

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()

			tarpit.Trap(ctx, serverConn, 10*time.Millisecond)
		}()

		reader := bufio.NewReader(clientConn)
		banner, err := reader.ReadString('\n')
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(banner, "SSH-2.0-AoniTarpit_"))
		assert.True(t, strings.HasSuffix(banner, "\r\n"))

		cancel()

		_ = clientConn.Close()

		wg.Wait()
	})

	t.Run("client_disconnect_triggers_exit", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		serverConn, clientConn := net.Pipe()

		done := make(chan struct{})
		go func() {
			tarpit.Trap(ctx, serverConn, 10*time.Millisecond)
			close(done)
		}()

		// Immediately close client conn to cause write error on server
		_ = clientConn.Close()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Trap did not exit when remote connection closed")
		}
	})
}

func TestZeroWindowFreeze(t *testing.T) {
	t.Parallel()

	t.Run("canceled_context_immediate", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		serverConn, clientConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			tarpit.ZeroWindowFreeze(ctx, serverConn, time.Minute)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("ZeroWindowFreeze did not exit on canceled context")
		}
	})

	t.Run("hold_duration_expiry", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		serverConn, clientConn := net.Pipe()
		defer clientConn.Close()

		done := make(chan struct{})
		go func() {
			// Test with very short hold duration to verify timer triggers exit
			tarpit.ZeroWindowFreeze(ctx, serverConn, 20*time.Millisecond)
			close(done)
		}()

		// Provide input so Read unblocks and freeze timer starts
		_, _ = clientConn.Write([]byte("SSH-2.0-Probe\r\n"))

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("ZeroWindowFreeze did not exit after hold duration expired")
		}
	})

	t.Run("initial_data_slurp", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		serverConn, clientConn := net.Pipe()

		done := make(chan struct{})
		go func() {
			tarpit.ZeroWindowFreeze(ctx, serverConn, time.Minute)
			close(done)
		}()

		// Write initial payload to be slurped by ZeroWindowFreeze
		_, err := clientConn.Write([]byte("SSH-2.0-OpenSSH_8.2p1\r\n"))
		require.NoError(t, err)

		cancel()

		_ = clientConn.Close()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("ZeroWindowFreeze did not exit cleanly after data write")
		}
	})
}
