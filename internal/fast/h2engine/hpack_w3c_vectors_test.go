// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine_test

import (
	"encoding/hex"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
)

// Official Test Vectors from RFC 7541 Appendix C (W3C HPACK Test Specification)

func hexToBytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}

	return b
}

func TestHPACK_W3C_RFC7541_AppendixC(t *testing.T) {
	t.Parallel()

	// Appendix C.2.1: Literal Header Field with Indexing
	// Name: custom-key, Value: custom-header
	t.Run("C.2.1_Literal_With_Indexing", func(t *testing.T) {
		encoded := hexToBytes("400a637573746f6d2d6b65790d637573746f6d2d686561646572")

		hp := h2engine.AcquireHPACK()
		defer h2engine.ReleaseHPACK(hp)

		fields, err := hp.DecodeAll(nil, encoded)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(fields))
		assert.Equal(t, "custom-key", fields[0].Key())
		assert.Equal(t, "custom-header", fields[0].Value())
	})

	// Appendix C.2.2: Literal Header Field without Indexing
	// Name: :path, Value: /my-example/index.html (Indexed Name index 4)
	t.Run("C.2.2_Literal_Without_Indexing", func(t *testing.T) {
		encoded := hexToBytes("04162f6d792d6578616d706c652f696e6465782e68746d6c")

		hp := h2engine.AcquireHPACK()
		defer h2engine.ReleaseHPACK(hp)

		fields, err := hp.DecodeAll(nil, encoded)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(fields))
		assert.Equal(t, ":path", fields[0].Key())
		assert.Equal(t, "/my-example/index.html", fields[0].Value())
	})

	// Appendix C.2.4: Indexed Header Field
	// Index 2: :method: GET
	t.Run("C.2.4_Indexed_Header_Field", func(t *testing.T) {
		encoded := hexToBytes("82")

		hp := h2engine.AcquireHPACK()
		defer h2engine.ReleaseHPACK(hp)

		fields, err := hp.DecodeAll(nil, encoded)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(fields))
		assert.Equal(t, ":method", fields[0].Key())
		assert.Equal(t, "GET", fields[0].Value())
	})

	// Appendix C.3: Request Examples with Huffman Coding (3 consecutive requests with dynamic table mutation)
	t.Run("C.3_Request_Huffman_Stream", func(t *testing.T) {
		hp := h2engine.AcquireHPACK()
		defer h2engine.ReleaseHPACK(hp)

		// Request 1:
		// :method: GET
		// :scheme: http
		// :path: /
		// :authority: www.example.com
		req1Bytes := hexToBytes("828684418cf1e3c2e5f23a6ba0ab90f4ff")
		fields1, err := hp.DecodeAll(nil, req1Bytes)
		assert.NoError(t, err)
		assert.Equal(t, 4, len(fields1))
		assert.Equal(t, "GET", fields1[0].Value())
		assert.Equal(t, "http", fields1[1].Value())
		assert.Equal(t, "/", fields1[2].Value())
		assert.Equal(t, "www.example.com", fields1[3].Value())

		// Request 2:
		// :method: GET
		// :scheme: http
		// :path: /
		// :authority: www.example.com
		// cache-control: no-cache
		req2Bytes := hexToBytes("828684be5886a8eb10649cbf")
		fields2, err := hp.DecodeAll(nil, req2Bytes)
		assert.NoError(t, err)
		assert.Equal(t, 5, len(fields2))
		assert.Equal(t, "GET", fields2[0].Value())
		assert.Equal(t, "no-cache", fields2[4].Value())

		// Request 3:
		// :method: GET
		// :scheme: https
		// :path: /index.html
		// :authority: www.example.com
		// custom-key: custom-value
		req3Bytes := hexToBytes("828785bf408825a849e95ba97d7f8925a849e95bb8e8b4bf")
		fields3, err := hp.DecodeAll(nil, req3Bytes)
		assert.NoError(t, err)
		assert.Equal(t, 5, len(fields3))
		assert.Equal(t, "GET", fields3[0].Value())
		assert.Equal(t, "https", fields3[1].Value())
		assert.Equal(t, "/index.html", fields3[2].Value())
		assert.Equal(t, "www.example.com", fields3[3].Value())
		assert.Equal(t, "custom-key", fields3[4].Key())
		assert.Equal(t, "custom-value", fields3[4].Value())
	})

	// Appendix C.5: Response Examples with Huffman Coding
	t.Run("C.5_Response_Huffman_Stream", func(t *testing.T) {
		hp := h2engine.AcquireHPACK()
		defer h2engine.ReleaseHPACK(hp)

		// Response 1:
		// :status: 302
		// cache-control: private
		// date: Mon, 21 Oct 2013 20:13:21 GMT
		// location: https://www.example.com
		resp1Bytes := hexToBytes(
			"488264025885aec3771a4b6196d07abe941054d444a8200595040b8166e082a62d1bff6e919d29ad171863c78f0b97c8e9ae82ae43d3",
		)
		fields, err := hp.DecodeAll(nil, resp1Bytes)
		assert.NoError(t, err)
		assert.Equal(t, 4, len(fields))
		assert.Equal(t, ":status", fields[0].Key())
		assert.Equal(t, "302", fields[0].Value())
		assert.Equal(t, "cache-control", fields[1].Key())
		assert.Equal(t, "private", fields[1].Value())
		assert.Equal(t, "date", fields[2].Key())
		assert.Equal(t, "Mon, 21 Oct 2013 20:13:21 GMT", fields[2].Value())
		assert.Equal(t, "location", fields[3].Key())
		assert.Equal(t, "https://www.example.com", fields[3].Value())
	})
}
