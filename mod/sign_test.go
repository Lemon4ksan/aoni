// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/foundation/bytesconv"
	"github.com/lemon4ksan/aoni/mod"
)

func TestWithSignHMAC(t *testing.T) {
	t.Parallel()

	req := newDummyRequest()
	req.SetMethod("POST")
	req.SetURL("https://api.binance.com/api/v3/order?symbol=BTCUSDT")
	req.SetBodyBytes([]byte(`{"price":50000,"quantity":1}`))

	secret := "secret-api-key"
	m := mod.WithSignHMAC(secret)
	m.Fn(req)

	ts := req.Header("X-Timestamp")
	require.NotEmpty(t, ts)

	sig := req.Header("X-Signature")
	require.NotEmpty(t, sig)

	// Validate HMAC calculation manually
	expectedPayload := ts + "POST" + req.URL() + string(req.BodyBytes())
	h := hmac.New(sha256.New, bytesconv.S2B(secret))
	h.Write(bytesconv.S2B(expectedPayload))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	require.Equal(t, expectedSig, sig)
}
