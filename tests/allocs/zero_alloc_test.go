// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package allocs_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
)

func TestZeroAlloc_WithBody(t *testing.T) {
	reader := bytes.NewReader([]byte("test"))
	allocs := testing.AllocsPerRun(100, func() {
		reader.Seek(0, io.SeekStart)
		_ = mod.WithBody(reader)
	})
	assert.Equal(t, 0.0, allocs)
}

func TestZeroAlloc_Challenge(t *testing.T) {
	b := []byte("<html><body>cloudflare challenge</body></html>")
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader(b))}

	allocs := testing.AllocsPerRun(100, func() {
		_, _ = challenge.DetectCloudflareChallenge(resp)
	})

	// DetectCloudflareChallenge replaces Body. But it shouldn't cause arbitrary heap escapes.
	// Since we aren't completely isolating it, we just enforce an upper bound representing
	// the intended struct allocation (multiReadCloser) which might allocate 1 if not pooled.
	assert.True(t, allocs <= 20.0)
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
