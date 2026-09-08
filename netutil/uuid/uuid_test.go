// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package uuid_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/uuid"
)

func TestUUIDFacade(t *testing.T) {
	t.Parallel()

	t.Run("v4_generation_and_validation", func(t *testing.T) {
		t.Parallel()

		u4, err := uuid.NewV4()
		require.NoError(t, err)
		assert.Equal(t, uuid.StringLength, len(u4.String()))
		assert.True(t, uuid.IsValid(u4.String()))

		mustU4 := uuid.MustNewV4()
		assert.True(t, uuid.IsValid(mustU4.String()))
	})

	t.Run("v7_generation_and_validation", func(t *testing.T) {
		t.Parallel()

		u7, err := uuid.NewV7()
		require.NoError(t, err)
		assert.Equal(t, uuid.StringLength, len(u7.String()))
		assert.True(t, uuid.IsValid(u7.String()))

		mustU7 := uuid.MustNewV7()
		assert.True(t, uuid.IsValid(mustU7.String()))
	})

	t.Run("parse_and_must_parse", func(t *testing.T) {
		t.Parallel()

		validStr := "018f4a12-8876-789a-bcde-f0123456789a"
		parsed, err := uuid.Parse(validStr)
		require.NoError(t, err)
		assert.Equal(t, validStr, parsed.String())

		mustParsed := uuid.MustParse(validStr)
		assert.Equal(t, parsed, mustParsed)

		_, err = uuid.Parse("invalid-uuid")
		require.Error(t, err)
	})

	t.Run("nil_and_max_constants", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "00000000-0000-0000-0000-000000000000", uuid.Nil.String())
		assert.Equal(t, "ffffffff-ffff-ffff-ffff-ffffffffffff", uuid.Max.String())
	})
}
