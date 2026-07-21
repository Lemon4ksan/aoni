// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Export internal unexported symbols for black-box testing in package aoni_test.

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
