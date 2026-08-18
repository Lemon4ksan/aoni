// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bytesconv_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/bytesconv"
)

func TestPatternSlicer_Slice(t *testing.T) {
	t.Parallel()

	slicer := bytesconv.NewPatternSlicer([]byte("KEY:"), 4)
	assert.NotNil(t, slicer)

	t.Run("pattern_found", func(t *testing.T) {
		t.Parallel()

		data := []byte("PREFIX_SECTION\r\nKEY: TARGET_PAYLOAD\r\n\r\n")
		chunks, matched := slicer.Slice(data)

		assert.True(t, matched)

		requireLen := 2
		assert.Len(t, chunks, requireLen)
		assert.Equal(t, "PREFIX_SECTION\r\nKEY:", string(chunks[0]))
		assert.Equal(t, " TARGET_PAYLOAD\r\n\r\n", string(chunks[1]))
	})

	t.Run("pattern_not_found", func(t *testing.T) {
		t.Parallel()

		data := []byte("PREFIX_SECTION\r\nOTHER_FIELD: test\r\n\r\n")
		chunks, matched := slicer.Slice(data)

		assert.False(t, matched)
		assert.Len(t, chunks, 1)
		assert.Equal(t, string(data), string(chunks[0]))
	})
}
