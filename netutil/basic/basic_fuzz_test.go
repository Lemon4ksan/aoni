// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package basic_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/basic"
)

func FuzzBasicAuth(f *testing.F) {
	f.Add("Basic YWxhZGRpbjpvcGVuc2VzYW1l", "Basic realm=\"WallyWorld\", charset=\"UTF-8\"")
	f.Add("Basic invalid_base64!@#", "Basic realm=\"test\"")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, authHeader, challengeHeader string) {
		u, p, ok := basic.Parse(authHeader)
		if ok {
			formatted := basic.Format(u, p)
			if len(formatted) == 0 {
				t.Fatalf("expected non-empty formatted auth")
			}
		}

		_, _ = basic.ParseChallenge(challengeHeader)
		_ = basic.InScope("https://example.com/api/v1", "https://example.com/api")
	})
}
