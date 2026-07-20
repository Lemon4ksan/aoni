// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lemon4ksan/miyako/sync/breaker"
)

// Export internal unexported symbols for black-box testing in package aoni_test.

type RawDecoderT = rawDecoder

func NewResponseBodyReadCloser(r io.ReadCloser) io.ReadCloser {
	return newResponseBodyReadCloser(r)
}

func NewMultiReadBody(r io.ReadCloser, threshold int64, disableDisk bool) (io.ReadCloser, error) {
	return newMultiReadBody(r, threshold, disableDisk)
}

func (cb *CircuitBreaker) GetBreaker(host string) *breaker.CircuitBreaker[any] {
	return cb.getBreaker(host)
}

func NewProgressReader(r io.Reader, total int64, fn ProgressFunc) io.Reader {
	return &progressReader{reader: r, total: total, onProgress: fn}
}

// SetupTestServer creates a test server and pre-configures a client With its URL.
// It registers resource cleanup automatically through t.Cleanup.
func SetupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewClient(nil)
	client.defaults.BaseURL, _ = url.Parse(server.URL)

	return server, client
}

func SetupBridgeTest(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c := NewClient(nil)
	c.defaults.BaseURL, _ = url.Parse(server.URL)
	stdClient := NewStdClient(c)

	return server, stdClient
}
