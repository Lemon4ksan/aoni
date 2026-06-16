// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_RawDecoderRecycling(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("raw payload data"))
	})

	var output []byte

	resp, err := client.Request(t.Context(), http.MethodGet, "/", AsRaw())
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	err = RawDecoder.Decode(resp.Body, &output)
	require.NoError(t, err)
	assert.Equal(t, "raw payload data", string(output))
}
