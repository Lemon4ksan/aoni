// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package digest_test

import (
	"crypto/md5" //nolint:gosec
	"encoding/hex"
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
	t.Parallel()

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

		assert.Contains(t, auth, `username="admin"`)
		assert.Contains(t, auth, `realm="TestRealm"`)
		assert.Contains(t, auth, `nonce="1234567890abcdef"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("digest success!"))
	}))
	t.Cleanup(server.Close)

	tr := &digest.Transport{
		Username:  username,
		Password:  password,
		Transport: server.Client().Transport,
	}

	client := &http.Client{Transport: tr}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "digest success!", string(body))
}

func TestDigestAuth_SHA256_AuthInt(t *testing.T) {
	t.Parallel()

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
	t.Cleanup(server.Close)

	tr := &digest.Transport{
		Username:  username,
		Password:  password,
		Transport: server.Client().Transport,
	}

	client := &http.Client{Transport: tr}
	payload := strings.NewReader(`{"hello":"digest"}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api", payload)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "auth-int success!", string(body))
}

func TestDigestAuth_UserHash(t *testing.T) {
	t.Parallel()

	const (
		username = "admin"
		password = "secretpassword"
		realm    = "TestRealm"
		nonce    = "1234567890abcdef"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().
				Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth", userhash=true`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		// Calculate expected userhash: hex(md5("admin:TestRealm"))
		h := md5.New()
		_, _ = h.Write([]byte("admin:TestRealm"))
		expectedUserHash := hex.EncodeToString(h.Sum(nil))

		assert.Contains(t, auth, fmt.Sprintf(`username="%s"`, expectedUserHash))
		assert.Contains(t, auth, `userhash=true`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("userhash ok"))
	}))
	t.Cleanup(server.Close)

	tr := &digest.Transport{
		Username:  username,
		Password:  password,
		Transport: server.Client().Transport,
	}

	client := &http.Client{Transport: tr}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/userhash", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "userhash ok", string(body))
}

func TestDigestAuth_ErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		wwwAuthenticate   string
		expectedErrSubstr string
	}{
		{
			name:              "bad_challenge_format",
			wwwAuthenticate:   `Basic realm="TestRealm"`,
			expectedErrSubstr: "bad challenge",
		},
		{
			name:              "unsupported_algorithm",
			wwwAuthenticate:   `Digest realm="TestRealm", nonce="123", algorithm="UNKNOWN-ALG"`,
			expectedErrSubstr: "algorithm not supported",
		},
		{
			name:              "unsupported_qop",
			wwwAuthenticate:   `Digest realm="TestRealm", nonce="123", qop="auth-conf"`,
			expectedErrSubstr: "qop not supported",
		},
		{
			name:              "invalid_charset",
			wwwAuthenticate:   `Digest realm="TestRealm", nonce="123", charset="ASCII"`,
			expectedErrSubstr: "invalid charset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("WWW-Authenticate", tt.wwwAuthenticate)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(server.Close)

			tr := &digest.Transport{
				Username:  "user",
				Password:  "pass",
				Transport: server.Client().Transport,
			}

			client := &http.Client{Transport: tr}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/", nil)
			require.NoError(t, err)

			_, err = client.Do(req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErrSubstr)
		})
	}
}
