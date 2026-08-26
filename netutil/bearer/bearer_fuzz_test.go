// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bearer_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/bearer"
)

func FuzzBearerAuth(f *testing.F) {
	f.Add("Bearer mF_9.B5f-4.1JqM", "Bearer realm=\"example\", error=\"invalid_token\", error_description=\"The access token expired\"")
	f.Add("Bearer token_with_specials!@#$%", "Bearer realm=\"test\"")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, authHeader, challengeHeader string) {
		token, ok := bearer.Parse(authHeader)
		if ok {
			_ = bearer.IsValidToken(token)
			formatted := bearer.Format(token)
			if len(formatted) == 0 {
				t.Fatalf("expected non-empty formatted bearer token")
			}
		}

		_, _ = bearer.ParseChallenge(challengeHeader)
	})
}
