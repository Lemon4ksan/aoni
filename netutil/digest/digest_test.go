// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package digest_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/digest"
)

func TestDigestAuth_MD5_Success(t *testing.T) {
	const (
		username = "admin"
		password = "secretpassword"
		realm    = "TestRealm"
		nonce    = "1234567890abcdef"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(auth, "Digest ") {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// Verify digest response
		assert.Contains(t, auth, `username="admin"`)
		assert.Contains(t, auth, `realm="TestRealm"`)
		assert.Contains(t, auth, `nonce="1234567890abcdef"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("digest success!"))
	}))
	defer server.Close()

	tr := &digest.Transport{
		Username:  username,
		Password:  password,
		Transport: server.Client().Transport,
	}

	client := &http.Client{Transport: tr}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/protected", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "digest success!", string(body))
}

func TestDigestAuth_SHA256_AuthInt(t *testing.T) {
	const (
		username = "user1"
		password = "pass123"
		realm    = "SecureRealm"
		nonce    = "fedcba0987654321"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().
				Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", algorithm="SHA-256", qop="auth-int"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		assert.Contains(t, auth, `algorithm=SHA-256`)
		assert.Contains(t, auth, `qop=auth-int`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("auth-int success!"))
	}))
	defer server.Close()

	tr := &digest.Transport{
		Username:  username,
		Password:  password,
		Transport: server.Client().Transport,
	}

	client := &http.Client{Transport: tr}
	payload := strings.NewReader(`{"hello":"digest"}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api", payload)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
