// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"strconv"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
)

// SignHMACConfig specifies configuration parameters for cryptographic HMAC request signing.
type SignHMACConfig struct {
	// Secret key used for signing.
	Secret string

	// HeaderName is the HTTP header where the signature is injected (default: "X-Signature").
	HeaderName string

	// TimestampHeader is the HTTP header where the unix timestamp in milliseconds is injected (default: "X-Timestamp").
	// If empty, timestamp header injection is skipped.
	TimestampHeader string

	// UseSHA512 selects SHA-512 instead of SHA-256 for the HMAC hash.
	UseSHA512 bool

	// Base64Encode formats the signature in Base64 rather than Hexadecimal encoding.
	Base64Encode bool

	// PayloadBuilder is an optional custom function to construct the bytes to be signed.
	// If nil, standard format is used: "<timestamp><METHOD><PATH><QUERY><BODY>".
	PayloadBuilder func(req aoni.Request, timestamp string) []byte
}

// WithSignHMAC constructs an [aoni.RequestModifier] injecting an HMAC-SHA256 signature
// into the "X-Signature" header and current Unix timestamp into "X-Timestamp".
func WithSignHMAC(secret string) aoni.RequestModifier {
	return WithSignHMACConfig(SignHMACConfig{
		Secret:          secret,
		HeaderName:      "X-Signature",
		TimestampHeader: "X-Timestamp",
	})
}

// WithSignHMACConfig constructs an [aoni.RequestModifier] applying custom HMAC request signing.
func WithSignHMACConfig(cfg SignHMACConfig) aoni.RequestModifier {
	headerName := generic.Coalesce(cfg.HeaderName, "X-Signature")

	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			var tsStr string
			if cfg.TimestampHeader != "" {
				ts := time.Now().UnixMilli()

				var tsBuf [32]byte

				tsStr = bytesconv.B2S(strconv.AppendInt(tsBuf[:0], ts, 10))
				req.SetHeader(cfg.TimestampHeader, tsStr)
			}

			var payload []byte
			if cfg.PayloadBuilder != nil {
				payload = cfg.PayloadBuilder(req, tsStr)
			} else {
				// Default signature payload: timestamp + method + path + query + body
				var buf []byte
				if tsStr != "" {
					buf = append(buf, tsStr...)
				}

				buf = append(buf, req.Method()...)
				if u := req.URL(); u != "" {
					buf = append(buf, u...)
				}

				if b := req.BodyBytes(); len(b) > 0 {
					buf = append(buf, b...)
				}

				payload = buf
			}

			var h hash.Hash
			if cfg.UseSHA512 {
				h = hmac.New(sha512.New, bytesconv.S2B(cfg.Secret))
			} else {
				h = hmac.New(sha256.New, bytesconv.S2B(cfg.Secret))
			}

			_, _ = h.Write(payload)
			sig := h.Sum(nil)

			var sigStr string
			if cfg.Base64Encode {
				sigStr = base64.StdEncoding.EncodeToString(sig)
			} else {
				sigStr = hex.EncodeToString(sig)
			}

			req.SetHeader(headerName, sigStr)
		},
	}
}
