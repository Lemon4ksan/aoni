// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webpush_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/x/webpush"
)

func decodeB64URL(t *testing.T, s string) []byte {
	t.Helper()
	s = strings.TrimSpace(s)
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}

	b, err := base64.URLEncoding.DecodeString(s)
	require.NoError(t, err)

	return b
}

func encodeB64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func TestRFC8291_AppendixA_Vector(t *testing.T) {
	t.Parallel()

	// Inputs from RFC 8291 Appendix A
	plaintext := []byte("When I grow up, I want to be a watermelon")
	authSecret := decodeB64URL(t, "BTBZMqHH6r4Tts7J_aSIgg")
	uaPublicB64 := "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	uaPrivateBytes := decodeB64URL(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94")
	asPrivateBytes := decodeB64URL(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw")
	salt := decodeB64URL(t, "DGv6ra1nlYgDCS1FRnbzlw")

	asPriv, err := ecdh.P256().NewPrivateKey(asPrivateBytes)
	require.NoError(t, err)

	uaPriv, err := ecdh.P256().NewPrivateKey(uaPrivateBytes)
	require.NoError(t, err)

	sub := &webpush.Subscription{
		Endpoint: "https://push.example.net/push/JzLQ3raZJfFBR0aqvOMsLrt54w4rJUsV",
		Keys: webpush.Keys{
			P256DH: uaPublicB64,
			Auth:   encodeB64URL(authSecret),
		},
	}

	cfg := &webpush.EncryptConfig{
		Salt:       salt,
		SenderPriv: asPriv,
		RecordSize: 4096,
	}

	encrypted, err := webpush.Encrypt(plaintext, sub, cfg)
	require.NoError(t, err)

	// Verify against RFC 8291 Appendix A full body
	expectedBodyB64 := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	expectedBody := decodeB64URL(t, expectedBodyB64)

	assert.Equal(t, expectedBody, encrypted)

	// Decrypt with recipient's private key
	decrypted, err := webpush.Decrypt(encrypted, uaPriv, authSecret)
	require.NoError(t, err)
	assert.Equal(t, string(plaintext), string(decrypted))
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	t.Parallel()

	uaPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)

	authSecret := make([]byte, 16)
	for i := range authSecret {
		authSecret[i] = byte(i + 1)
	}

	sub := &webpush.Subscription{
		Endpoint: "https://web.push.apple.com/send/token123",
		Keys: webpush.Keys{
			P256DH: encodeB64URL(uaPriv.PublicKey().Bytes()),
			Auth:   encodeB64URL(authSecret),
		},
	}

	messages := []string{
		"Hello iPhone!",
		`{"title":"Order Shipped","body":"Your package is on its way"}`,
		strings.Repeat("Aoni Sovereign WebPush ", 50),
	}

	for _, msg := range messages {
		encrypted, err := webpush.Encrypt([]byte(msg), sub, nil)
		require.NoError(t, err)

		decrypted, err := webpush.Decrypt(encrypted, uaPriv, authSecret)
		require.NoError(t, err)
		assert.Equal(t, msg, string(decrypted))
	}
}

func TestVAPID_SignAndVerify(t *testing.T) {
	t.Parallel()

	keys, err := webpush.GenerateVAPIDKeys()
	require.NoError(t, err)
	require.NotEmpty(t, keys.PublicKeyBase64())
	require.NotEmpty(t, keys.PrivateKeyBase64())

	// Reconstruct keys from private/public base64
	reconstructed, err := webpush.NewVAPIDKeys(keys.PrivateKeyBase64(), keys.PublicKeyBase64())
	require.NoError(t, err)
	assert.Equal(t, keys.PublicKeyBase64(), reconstructed.PublicKeyBase64())

	authHeader, err := keys.AuthorizationHeader("https://fcm.googleapis.com/fcm/send/token", "mailto:admin@example.com", time.Hour)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(authHeader, "vapid t="))
	assert.Contains(t, authHeader, ", k=")
	assert.Contains(t, authHeader, keys.PublicKeyBase64())
}

type PushNotificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Badge int    `json:"badge"`
}

func TestClient_E2E_PushDelivery(t *testing.T) {
	t.Parallel()

	uaPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)

	authSecret := make([]byte, 16)
	for i := range authSecret {
		authSecret[i] = byte(0xaa + i)
	}

	vapidKeys, err := webpush.GenerateVAPIDKeys()
	require.NoError(t, err)

	var receivedBody []byte
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		receivedBody = body

		w.Header().Set("Location", "https://push.example.net/message/msg123")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	sub := &webpush.Subscription{
		Endpoint: server.URL + "/push/sub123",
		Keys: webpush.Keys{
			P256DH: encodeB64URL(uaPriv.PublicKey().Bytes()),
			Auth:   encodeB64URL(authSecret),
		},
	}

	vapidConfig := &webpush.VAPIDConfig{
		Keys:    vapidKeys,
		Subject: "mailto:ops@aoni.dev",
		TTL:     2 * time.Hour,
	}

	pushClient := webpush.NewClient(aoni.NewClient(nil), vapidConfig)

	payload := PushNotificationPayload{
		Title: "Breaking News",
		Body:  "Aoni Sovereign WebPush delivers instant alerts!",
		Badge: 1,
	}

	resp, err := pushClient.SendJSON(
		context.Background(),
		sub,
		payload,
		webpush.WithTTL(60*time.Second),
		webpush.WithUrgency(webpush.UrgencyHigh),
		webpush.WithTopic("breaking-news"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// Verify headers per RFC 8030 / RFC 8291 / RFC 8292
	assert.Equal(t, "60", receivedHeaders.Get(webpush.HeaderTTL))
	assert.Equal(t, "high", receivedHeaders.Get(webpush.HeaderUrgency))
	assert.Equal(t, "breaking-news", receivedHeaders.Get(webpush.HeaderTopic))
	assert.Equal(t, webpush.ContentEncodingAES128GCM, receivedHeaders.Get(webpush.HeaderContentEncoding))
	assert.True(t, strings.HasPrefix(receivedHeaders.Get(webpush.HeaderAuthorization), "vapid t="))

	// Decrypt received body
	decrypted, err := webpush.Decrypt(receivedBody, uaPriv, authSecret)
	require.NoError(t, err)
	assert.Contains(t, string(decrypted), "Breaking News")
}
