// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package digest_test

import (
	"crypto/md5" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
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

func TestRFC7616_Section3_9_1_TestVectors(t *testing.T) {
	t.Parallel()

	// RFC 7616 §3.9.1 parameters
	const (
		username = "Mufasa"
		password = "Circle of Life"
		realm    = "http-auth@example.org"
		uri      = "/dir/index.html"
		nonce    = "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v"
		opaque   = "FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"
		cnonce   = "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ"
		nc       = "00000001"
		qop      = "auth"
	)

	// MD5 Vector Verification
	// A1 = Mufasa:http-auth@example.org:Circle of Life
	// H(A1) = md5(Mufasa:http-auth@example.org:Circle of Life) = 939e7578ed9e3c518a452acee763bce9
	// A2 = GET:/dir/index.html
	// H(A2) = md5(GET:/dir/index.html) = 39da92e336e6e7ca48000850491d746d
	// response = md5(939e7578ed9e3c518a452acee763bce9:7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v:00000001:f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ:auth:39da92e336e6e7ca48000850491d746d)
	// Expected MD5 response: 8ca523f5e9506fed4657c9700eebdbec
	ha1MD5 := hex.EncodeToString(func() []byte {
		h := md5.New() //nolint:gosec
		h.Write([]byte(username + ":" + realm + ":" + password))
		return h.Sum(nil)
	}())
	ha2MD5 := hex.EncodeToString(func() []byte {
		h := md5.New() //nolint:gosec
		h.Write([]byte("GET:" + uri))
		return h.Sum(nil)
	}())
	respMD5 := hex.EncodeToString(func() []byte {
		h := md5.New() //nolint:gosec
		h.Write([]byte(ha1MD5 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2MD5))
		return h.Sum(nil)
	}())
	assert.Equal(t, "8ca523f5e9506fed4657c9700eebdbec", respMD5)

	// SHA-256 Vector Verification (RFC 7616 §3.9.1)
	// Expected SHA-256 response: 753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1
	ha1SHA256 := hex.EncodeToString(func() []byte {
		h := sha256.New()
		h.Write([]byte(username + ":" + realm + ":" + password))
		return h.Sum(nil)
	}())
	ha2SHA256 := hex.EncodeToString(func() []byte {
		h := sha256.New()
		h.Write([]byte("GET:" + uri))
		return h.Sum(nil)
	}())
	respSHA256 := hex.EncodeToString(func() []byte {
		h := sha256.New()
		h.Write([]byte(ha1SHA256 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2SHA256))
		return h.Sum(nil)
	}())
	assert.Equal(t, "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1", respSHA256)
}

func TestRFC7616_Section3_9_2_TestVectors(t *testing.T) {
	t.Parallel()

	// RFC 7616 §3.9.2: SHA-512-256, Unicode username "Jäsøn Doe" (per RFC Erratum 4897)
	const (
		username = "Jäsøn Doe"
		password = "Secret, or not?"
		realm    = "api@example.org"
		uri      = "/doe.json"
		nonce    = "5TsQWLVdgBdmrQ0XsxbDODV+57QdFR34I9HAbC/RVvkK"
		cnonce   = "NTg6RKcb9boFIAS3KrFK9BGeh+iDa/sm6jUMp2wds69v"
		nc       = "00000001"
		qop      = "auth"
	)

	// Userhash calculation: hex(sha512_256("Jäsøn Doe:api@example.org"))
	// Verified via RFC 7616 Erratum 4897
	userhash := hex.EncodeToString(func() []byte {
		h := sha512.New512_256()
		h.Write([]byte(username + ":" + realm))
		return h.Sum(nil)
	}())
	assert.Equal(t, "793263caabb707a56211940d90411ea4a575adeccb7e360aeb624ed06ece9b0b", userhash)

	// Response calculation:
	// A1 = Jäsøn Doe:api@example.org:Secret, or not?
	// A2 = GET:/doe.json
	// Verified via RFC 7616 Erratum 4897
	ha1 := hex.EncodeToString(func() []byte {
		h := sha512.New512_256()
		h.Write([]byte(username + ":" + realm + ":" + password))
		return h.Sum(nil)
	}())
	ha2 := hex.EncodeToString(func() []byte {
		h := sha512.New512_256()
		h.Write([]byte("GET:" + uri))
		return h.Sum(nil)
	}())
	resp := hex.EncodeToString(func() []byte {
		h := sha512.New512_256()
		h.Write([]byte(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2))
		return h.Sum(nil)
	}())
	assert.Equal(t, "3798d4131c277846293534c3edc11bd8a5e4cdcbff78b05db9d95eeb1cec68a5", resp)
}
