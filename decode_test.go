// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_RawDecoderRecycling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("raw payload data"))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	var output []byte

	resp, err := client.Request(context.Background(), http.MethodGet, "/", AsRaw())
	require.NoError(t, err)

	defer resp.Body.Close()

	err = RawDecoder.Decode(resp.Body, &output)
	require.NoError(t, err)
	assert.Equal(t, "raw payload data", string(output))
}
