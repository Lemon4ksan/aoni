// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package security_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/realtime/stream"
)

func TestSecurity_GRPCWeb_OOM(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc-web+proto")
		w.Write([]byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF})
	}))
	defer ts.Close()

	client := aoni.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	respStream, err := stream.Get(ctx, client, ts.URL)
	assert.NoError(t, err)
	defer respStream.Close()

	reader := stream.IterGRPCWeb[struct{}](respStream)

	count := 0
	for _, err := range reader {
		count++
		assert.Error(t, err)
	}
	assert.Equal(t, 1, count)
}

func TestSecurity_NDJSON_OOM(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for range 20 * 1024 {
			w.Write(bytes.Repeat([]byte{65}, 1024))
		}
	}))
	defer ts.Close()

	client := aoni.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	respStream, err := stream.Get(ctx, client, ts.URL)
	assert.NoError(t, err)
	defer respStream.Close()

	reader := stream.IterNDJSON[struct{}](respStream)

	for _, err := range reader {
		assert.Error(t, err)
		break
	}
}
