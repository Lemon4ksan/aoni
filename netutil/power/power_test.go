// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package power

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestWatcher_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("default_jump_threshold", func(t *testing.T) {
		t.Parallel()

		w := NewWatcher(0)

		require.NotNil(t, w)
		defer w.Close()

		assert.Equal(t, 5*time.Second, w.jumpThreshold)
	})

	t.Run("custom_jump_threshold", func(t *testing.T) {
		t.Parallel()

		w := NewWatcher(10 * time.Second)

		require.NotNil(t, w)
		defer w.Close()

		assert.Equal(t, 10*time.Second, w.jumpThreshold)
	})

	t.Run("nil_callbacks_ignored", func(t *testing.T) {
		t.Parallel()

		w := NewWatcher(time.Second)
		defer w.Close()

		// Verifies nil callbacks do not panic
		w.OnSuspend(nil)
		w.OnResume(nil)
	})

	t.Run("notify_suspend_and_resume_execution", func(t *testing.T) {
		t.Parallel()

		w := NewWatcher(time.Second)
		defer w.Close()

		var (
			suspendCalled atomic.Int32
			resumeCalled  atomic.Int32
		)

		w.OnSuspend(func() {
			suspendCalled.Add(1)
		})
		w.OnSuspend(func() {
			suspendCalled.Add(1)
		})

		w.OnResume(func() {
			resumeCalled.Add(1)
		})

		w.notifySuspendAndResume()

		assert.Equal(t, int32(2), suspendCalled.Load())
		assert.Equal(t, int32(1), resumeCalled.Load())
	})
}
