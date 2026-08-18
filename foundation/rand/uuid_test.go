// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rand_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/foundation/rand"
)

func TestUUIDv7(t *testing.T) {
	t.Parallel()

	u1 := rand.UUIDv7()
	require.Len(t, u1, 36)
	require.Equal(t, byte('7'), u1[14], "version must be 7")
	require.Contains(t, []byte{'8', '9', 'a', 'b'}, u1[19], "variant must be RFC 4122/9562")

	time.Sleep(2 * time.Millisecond)

	u2 := rand.UUIDv7()
	require.NotEqual(t, u1, u2)
	require.True(t, u2 > u1, "UUIDv7 should be monotonically orderable by time")
}

func BenchmarkUUIDv7(b *testing.B) {
	var buf [36]byte

	now := time.Now()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = rand.AppendUUIDv7(buf[:0], now)
	}
}
