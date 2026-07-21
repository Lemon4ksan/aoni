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
)

func TestClientWithPersona(t *testing.T) {
	c := NewClient(nil)

	c2 := c.WithPersona(PersonaChrome120Windows)

	assert.Equal(t, DefaultUserAgent, c2.Defaults().Headers.Get("User-Agent"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, DefaultUserAgent, r.UserAgent())
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := c2.Request(context.Background(), http.MethodGet, server.URL)
	assert.NoError(t, err)

	if resp != nil {
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	}
}

func TestPredefinedPersonas(t *testing.T) {
	personas := []Persona{
		PersonaChrome120Windows,
		PersonaChrome120Android,
		PersonaFirefox120Windows,
		PersonaFirefox120Android,
		PersonaSafari17MacOS,
		PersonaSafari17IOS,
	}

	for _, p := range personas {
		assert.NotEmpty(t, p.UserAgent)
		assert.NotEmpty(t, p.HeaderOrder)
		assert.NotNil(t, p.P0fSignature)
		assert.NotEmpty(t, p.TLSID.Client)
		assert.NotEmpty(t, p.TLSID.Version)
	}
}
