// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bytes"
	"testing"
)

func TestHuffmanEncodingSymmetry(t *testing.T) {
	inputs := []string{
		"aoni",
		":method",
		"GET",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	}

	for _, input := range inputs {
		src := []byte(input)

		encoded := HuffmanEncode(nil, src)
		decoded := HuffmanDecode(nil, encoded)

		if !bytes.Equal(decoded, src) {
			t.Fatalf("Huffman decoding failure for %q: got %q", input, string(decoded))
		}
	}
}

func TestHPACKEncodeDecodeSymmetry(t *testing.T) {
	hpEnc := AcquireHPACK()
	defer ReleaseHPACK(hpEnc)

	hpDec := AcquireHPACK()
	defer ReleaseHPACK(hpDec)

	headersToTest := []struct {
		key   string
		value string
	}{
		{":method", "GET"},
		{":scheme", "https"},
		{":path", "/api/v1/users"},
		{":authority", "example.com"},
		{"user-agent", "aoni-custom-agent"},
		{"x-custom-header", "custom-value-123"},
	}

	hFrame := AcquireFrame(FrameHeaders).(*Headers)
	hf := AcquireHeaderField()

	defer ReleaseHeaderField(hf)

	for _, h := range headersToTest {
		hf.Set(h.key, h.value)
		hFrame.AppendHeaderField(hpEnc, hf, true)
	}

	rawHeaders := hFrame.Headers()

	decodedHeaders := make(map[string]string)
	currBuf := rawHeaders

	for len(currBuf) > 0 {
		hfRecv := AcquireHeaderField()

		var err error

		currBuf, err = hpDec.Next(hfRecv, currBuf)
		if err != nil {
			ReleaseHeaderField(hfRecv)
			t.Fatalf("failed to decode HPACK stream: %v", err)
		}

		decodedHeaders[hfRecv.Key()] = hfRecv.Value()
		ReleaseHeaderField(hfRecv)
	}

	for _, expected := range headersToTest {
		val, ok := decodedHeaders[expected.key]
		if !ok {
			t.Errorf("missing header in decoded map: %s", expected.key)
			continue
		}

		if val != expected.value {
			t.Errorf("header value mismatch for %s: got %q, want %q", expected.key, val, expected.value)
		}
	}
}

func TestHPACKDynamicTableShrinking(t *testing.T) {
	hp := AcquireHPACK()
	defer ReleaseHPACK(hp)

	hp.SetMaxTableSize(100)

	for range 10 {
		hf := AcquireHeaderField()

		hf.Set("x-large-header-key", "some-very-large-value-that-causes-table-eviction")
		hp.addDynamic(hf)

		ReleaseHeaderField(hf)
	}

	if hp.DynamicSize() > 100 {
		t.Fatalf("dynamic table size (%d) exceeded max size limit (100)", hp.DynamicSize())
	}
}
