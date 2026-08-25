// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine_test

import (
	"bufio"
	"bytes"
	"io"
	"testing"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/foundation/testkit/assert"
)

// Official test cases adapted from nodejs/http-parser and nodejs/llhttp
// (C standard HTTP parser benchmark & fuzz corpus).

func TestLLHTTP_Chunked_OfficialVectors(t *testing.T) {
	t.Parallel()

	testVectors := []struct {
		name        string
		rawStream   string
		expected    string
		shouldError bool
	}{
		{
			name:      "simple chunked",
			rawStream: "4\r\nWiki\r\n5\r\npedia\r\nf\r\n in \r\n\r\nchunks.\r\n0\r\n\r\n",
			expected:  "Wikipedia in \r\n\r\nchunks.",
		},
		{
			name:      "chunks with extensions",
			rawStream: "4;foo=bar\r\nWiki\r\n5;baz\r\npedia\r\n0;end=true\r\n\r\n",
			expected:  "Wikipedia",
		},
		{
			name:      "chunks with quoted extensions and whitespace",
			rawStream: "4;foo=\"bar;baz\"\r\nWiki\r\n0\r\n\r\n",
			expected:  "Wiki",
		},
		{
			name:      "chunk with trailers (RFC 9112 §7.1.2)",
			rawStream: "4\r\nWiki\r\n0\r\nExpires: Wed, 21 Oct 2026 07:28:00 GMT\r\nETag: \"737060cd8c284d4329360fe080e4b0e9\"\r\n\r\n",
			expected:  "Wiki",
		},
		{
			name:      "leading zeros in chunk size",
			rawStream: "00000004\r\nWiki\r\n00000000\r\n\r\n",
			expected:  "Wiki",
		},
		{
			name:      "single byte chunks",
			rawStream: "1\r\na\r\n1\r\nb\r\n1\r\nc\r\n0\r\n\r\n",
			expected:  "abc",
		},
		{
			name:      "uppercase hex sizes",
			rawStream: "A\r\n0123456789\r\nB\r\n0123456789A\r\n0\r\n\r\n",
			expected:  "01234567890123456789A",
		},
		{
			name:        "invalid hex in chunk size (error)",
			rawStream:   "4g\r\nWiki\r\n0\r\n\r\n",
			shouldError: true,
		},
		{
			name:        "signed chunk size (error)",
			rawStream:   "+4\r\nWiki\r\n0\r\n\r\n",
			shouldError: true,
		},
		{
			name:        "negative chunk size (error)",
			rawStream:   "-4\r\nWiki\r\n0\r\n\r\n",
			shouldError: true,
		},
		{
			name:        "chunk length overflow > 16 hex digits (error)",
			rawStream:   "10000000000000000\r\nWiki\r\n0\r\n\r\n",
			shouldError: true,
		},
		{
			name:        "missing CRLF after chunk data (error)",
			rawStream:   "4\r\nWiki0\r\n\r\n",
			shouldError: true,
		},
	}

	for _, tv := range testVectors {
		t.Run(tv.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewBufferString(tv.rawStream))
			decoded, err := h1engine.ReadBodyChunked(r, 0, nil)

			if tv.shouldError {
				assert.Equal(t, true, err != nil && err != io.EOF)
			} else {
				if err != nil && err != io.EOF {
					t.Fatalf("unexpected chunk decoding error: %v", err)
				}
				assert.Equal(t, tv.expected, string(decoded))
			}
		})
	}
}
