// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package etag_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/resiliency/etag"
)

func TestETagAutomaton(t *testing.T) {
	t.Parallel()

	auto := etag.NewAutomaton()
	key := "https://api.example.com/data"

	// 1. Initial 200 OK with ETag
	initResp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		Header: http.Header{
			"Etag": []string{`"v1.0.0"`},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(`{"version":"1.0.0"}`))),
	}

	auto.Record(key, `"v1.0.0"`, initResp, []byte(`{"version":"1.0.0"}`))

	// 2. Verify stored ETag lookup
	storedETag := auto.GetETag(key)
	require.Equal(t, `"v1.0.0"`, storedETag)

	// 3. Reconstruct cached body on 304
	reconstructed := auto.Reconstruct304(key)
	require.NotNil(t, reconstructed)
	require.Equal(t, http.StatusOK, reconstructed.StatusCode)

	body, err := io.ReadAll(reconstructed.Body)
	require.NoError(t, err)
	require.Equal(t, `{"version":"1.0.0"}`, string(body))
}
