// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oracle

import (
	"context"
	"strings"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

// Injector extracts content from a request to produce an attestation token,
// and mutates the request with the resulting signature and headers.
type Injector func(ctx context.Context, r aoni.Request, resp *TokenResponse) error

// WithOracle returns an aoni.RequestModifier that retrieves an attestation token
// from the Oracle client and applies the intercepted headers, cookies, and signature.
func WithOracle(client *Client, prompt, headerKey string) aoni.RequestModifier {
	return mod.Custom(func(r aoni.Request) {
		if client == nil {
			return
		}

		ctx := r.Context()

		tokenResp, err := client.GetToken(ctx, prompt)
		if err != nil || tokenResp == nil {
			return
		}

		// Apply captured headers
		for k, v := range tokenResp.Headers {
			if strings.EqualFold(k, "content-length") || strings.EqualFold(k, "host") {
				continue
			}

			r.SetHeader(k, v)
		}

		// Apply captured cookies if present
		if tokenResp.Cookies != "" && r.Header("Cookie") == "" {
			r.SetHeader("Cookie", tokenResp.Cookies)
		}

		// Apply signature token to specified header if provided
		if headerKey != "" && tokenResp.Token != "" {
			r.SetHeader(headerKey, tokenResp.Token)
		}
	})
}
