// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ca_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/ca"
)

func generateUserPublicKey(t *testing.T) golangssh.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPubKey, err := golangssh.NewPublicKey(pub)
	require.NoError(t, err)

	return sshPubKey
}

func TestCA_GenerateAndLoadKeys(t *testing.T) {
	t.Parallel()

	// 1. Generate CA
	caObj, pemBytes, err := ca.GenerateCA()
	require.NoError(t, err)
	require.NotNil(t, caObj)
	require.NotEmpty(t, pemBytes)

	pubKey := caObj.PublicKey()
	require.NotNil(t, pubKey)

	authKey := caObj.AuthorizedKey()
	assert.Contains(t, string(authKey), "ssh-ed25519")

	// 2. Load CA from PEM bytes
	caFromPEM, err := ca.NewCAFromPEM(pemBytes, "")
	require.NoError(t, err)
	assert.Equal(t, pubKey.Marshal(), caFromPEM.PublicKey().Marshal())

	// 3. Load CA from file path
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "ca_key.pem")
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	caFromFile, err := ca.NewCAFromFile(keyPath, "")
	require.NoError(t, err)
	assert.Equal(t, pubKey.Marshal(), caFromFile.PublicKey().Marshal())

	// 4. Save CA public key to disk
	err = caObj.SaveCAKeys(tempDir)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(tempDir, "ca_key.pub"))
}

func TestCA_IssueUserCert(t *testing.T) {
	t.Parallel()

	caObj, _, err := ca.GenerateCA()
	require.NoError(t, err)

	userKey := generateUserPublicKey(t)

	t.Run("valid_user_certificate", func(t *testing.T) {
		t.Parallel()

		cert, err := caObj.IssueUserCert(userKey, "developer",
			ca.WithTTL(2*time.Hour),
			ca.WithPrincipals("developer", "root"),
			ca.WithKeyID("key-audit-101"),
			ca.WithSerial(9999),
			ca.WithExtensions(map[string]string{"permit-pty": ""}),
			ca.WithCriticalOptions(map[string]string{"force-command": "/bin/bash"}),
		)

		require.NoError(t, err)
		require.NotNil(t, cert)

		assert.Equal(t, uint32(golangssh.UserCert), cert.CertType)
		assert.Equal(t, "key-audit-101", cert.KeyId)
		assert.Equal(t, uint64(9999), cert.Serial)
		assert.Contains(t, cert.ValidPrincipals, "developer")
		assert.Contains(t, cert.ValidPrincipals, "root")
		assert.Contains(t, cert.Extensions, "permit-pty")
		assert.Equal(t, "/bin/bash", cert.CriticalOptions["force-command"])
	})

	t.Run("nil_public_key_fails", func(t *testing.T) {
		t.Parallel()

		_, err := caObj.IssueUserCert(nil, "user")
		assert.ErrorIs(t, err, ca.ErrInvalidPublicKey)
	})
}

func TestCA_IssueHostCert(t *testing.T) {
	t.Parallel()

	caObj, _, err := ca.GenerateCA()
	require.NoError(t, err)

	hostKey := generateUserPublicKey(t)

	t.Run("valid_host_certificate", func(t *testing.T) {
		t.Parallel()

		cert, err := caObj.IssueHostCert(hostKey, "node1.cluster.local")
		require.NoError(t, err)
		require.NotNil(t, cert)

		assert.Equal(t, uint32(golangssh.HostCert), cert.CertType)
		assert.Contains(t, cert.ValidPrincipals, "node1.cluster.local")
		assert.Contains(t, cert.KeyId, "aoni-host-cert-node1.cluster.local")
	})

	t.Run("nil_host_key_fails", func(t *testing.T) {
		t.Parallel()

		_, err := caObj.IssueHostCert(nil, "host")
		assert.ErrorIs(t, err, ca.ErrInvalidPublicKey)
	})
}

func TestCA_IssueFromOIDCToken(t *testing.T) {
	t.Parallel()

	caObj, _, err := ca.GenerateCA()
	require.NoError(t, err)

	userKey := generateUserPublicKey(t)

	buildJWT := func(claims map[string]any) string {
		hdrJSON, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
		claimsJSON, _ := json.Marshal(claims)

		hdrB64 := base64.RawURLEncoding.EncodeToString(hdrJSON)
		claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

		return hdrB64 + "." + claimsB64 + ".sig"
	}

	t.Run("valid_oidc_jwt_issuance", func(t *testing.T) {
		t.Parallel()

		jwtStr := buildJWT(map[string]any{
			"iss":   "https://auth.company.com",
			"email": "alice@company.com",
			"exp":   float64(time.Now().Add(1 * time.Hour).Unix()),
		})

		oidcCfg := ca.OIDCConfig{
			Issuer:    "https://auth.company.com",
			ClaimName: "email",
		}

		cert, username, err := caObj.IssueFromOIDCToken(t.Context(), jwtStr, userKey, oidcCfg)
		require.NoError(t, err)
		assert.Equal(t, "alice@company.com", username)
		assert.Contains(t, cert.ValidPrincipals, "alice@company.com")
	})

	t.Run("fallback_to_sub_claim_when_email_missing", func(t *testing.T) {
		t.Parallel()

		jwtStr := buildJWT(map[string]any{
			"iss": "https://auth.company.com",
			"sub": "user_sub_999",
			"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		})

		oidcCfg := ca.OIDCConfig{
			Issuer:    "https://auth.company.com",
			ClaimName: "email",
		}

		cert, username, err := caObj.IssueFromOIDCToken(t.Context(), jwtStr, userKey, oidcCfg)
		require.NoError(t, err)
		assert.Equal(t, "user_sub_999", username)
		assert.Contains(t, cert.ValidPrincipals, "user_sub_999")
	})

	t.Run("expired_jwt_token_fails", func(t *testing.T) {
		t.Parallel()

		jwtStr := buildJWT(map[string]any{
			"iss":   "https://auth.company.com",
			"email": "alice@company.com",
			"exp":   float64(time.Now().Add(-1 * time.Hour).Unix()),
		})

		oidcCfg := ca.OIDCConfig{
			Issuer: "https://auth.company.com",
		}

		_, _, err := caObj.IssueFromOIDCToken(t.Context(), jwtStr, userKey, oidcCfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ca.ErrCertExpired)
	})

	t.Run("invalid_jwt_format_fails", func(t *testing.T) {
		t.Parallel()

		oidcCfg := ca.OIDCConfig{Issuer: "https://auth.company.com"}
		_, _, err := caObj.IssueFromOIDCToken(t.Context(), "invalid.jwt.token.parts", userKey, oidcCfg)
		require.Error(t, err)
		assert.ErrorIs(t, err, ca.ErrInvalidOIDCToken)
	})
}
