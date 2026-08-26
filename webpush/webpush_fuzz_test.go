// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webpush_test

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/webpush"
)

func FuzzVAPIDKeys(f *testing.F) {
	keys, _ := webpush.GenerateVAPIDKeys()
	f.Add(keys.PrivateKeyBase64(), keys.PublicKeyBase64(), "https://updates.push.services.mozilla.com/wpush/v2/sub", "mailto:admin@example.com")
	f.Add("", "", "", "")
	f.Add("invalid_priv", "invalid_pub", "not_a_url", "admin")

	f.Fuzz(func(t *testing.T, privB64, pubB64, endpoint, subject string) {
		k, err := webpush.NewVAPIDKeys(privB64, pubB64)
		if err == nil && k != nil {
			_ = k.PublicKeyBase64()
			_ = k.PrivateKeyBase64()
			_, _ = k.SignJWT(endpoint, subject, 12*time.Hour)
			_, _ = k.AuthorizationHeader(endpoint, subject, 12*time.Hour)
		}
	})
}

func FuzzWebPushDecrypt(f *testing.F) {
	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	authSecret := make([]byte, 16)
	_, _ = rand.Read(authSecret)

	sub := &webpush.Subscription{
		Endpoint: "https://fcm.googleapis.com/fcm/send/sample",
		Keys: webpush.Keys{
			P256DH: keysToBase64(uaPriv.PublicKey().Bytes()),
			Auth:   keysToBase64(authSecret),
		},
	}

	encrypted, _ := webpush.Encrypt([]byte("fuzz test plaintext payload"), sub, nil)

	f.Add(encrypted, authSecret)
	f.Add([]byte{}, []byte{})
	f.Add([]byte("short encrypted text"), []byte("1234567890123456"))

	f.Fuzz(func(t *testing.T, payload, auth []byte) {
		_, _ = webpush.Decrypt(payload, uaPriv, auth)
	})
}

func keysToBase64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
