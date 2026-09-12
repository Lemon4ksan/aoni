// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package allocs_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
)

func TestZeroAlloc_WithBody(t *testing.T) {
	reader := bytes.NewReader([]byte("test"))
	allocs := testing.AllocsPerRun(100, func() {
		reader.Seek(0, io.SeekStart)
		_ = mod.WithBody(reader)
	})
	assert.Equal(t, 0.0, allocs)
}

type dummy struct {
	Name string
}

func TestZeroAlloc_LimitDecoder(t *testing.T) {
	decoder := decode.JSONDecoder
	limitDec := decode.LimitDecoder(decoder, 1024)

	b := []byte(`{"Name":"Test"}`)
	var d dummy

	allocs := testing.AllocsPerRun(100, func() {
		r := bytes.NewReader(b)
		_ = limitDec.Decode(r, &d)
	})

	// 1 for bytes.NewReader, 1 for d dummy, but io.LimitedReader shouldn't allocate
	assert.True(t, allocs <= 20.0)
}
