// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package p0f_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/fingerprint/p0f"
)

func FuzzParseP0fSignature(f *testing.F) {
	f.Add("*:64:0:*:mss*20,10:mss,sok,ts,nop,ws:df,id+:0")
	f.Add("4:128:0:1460:8192,2:mss,nop,ws,nop,nop,sok:df,id+:0")
	f.Add("6:64-:0:*:65535,6:mss,sok,ts,nop,ws:df+:0")
	f.Add("*:64:0:*:*,-1:mss,sok,ts:ack+:0")
	f.Add("*:64:0:*:mtu*4,0:mss,sok:df:0")
	f.Add("")
	f.Add("invalid:signature:with:fewer:fields")
	f.Add("*:not_a_number:0:*:16384,0:mss:df:0")

	f.Fuzz(func(t *testing.T, sig string) {
		s, err := p0f.Parse(sig)
		if err == nil && s != nil {
			str := s.String()
			if len(str) == 0 {
				t.Fatalf("expected non-empty reconstructed signature")
			}

			s2, err2 := p0f.Parse(str)
			if err2 != nil || s2.IPVersion != s.IPVersion || s2.TTL != s.TTL || s2.WindowType != s.WindowType {
				t.Fatalf("p0f roundtrip mismatch: got %v, expected %v", s2, s)
			}
		}
	})
}
