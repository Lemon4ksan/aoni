// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package uuid_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/uuid"
)

func TestUUIDFacade(t *testing.T) {
	t.Parallel()

	u4 := uuid.MustNewV4()
	assert.True(t, uuid.IsValid(u4.String()))

	u7 := uuid.MustNewV7()
	assert.True(t, uuid.IsValid(u7.String()))

	parsed, err := uuid.Parse(u4.String())
	require.NoError(t, err)
	assert.Equal(t, u4, parsed)
}
