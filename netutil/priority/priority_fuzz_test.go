// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package priority_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/netutil/priority"
)

func FuzzParsePriority(f *testing.F) {
	f.Add("u=1, i")
	f.Add("u=0")
	f.Add("i")
	f.Add("i=?1")
	f.Add("i=?0")
	f.Add("u=7, i=?1")
	f.Add("")
	f.Add("u=999, i=maybe")
	f.Add("malformed; priority=header, u=-5")

	f.Fuzz(func(t *testing.T, raw string) {
		p, err := priority.Parse(raw)
		if err == nil {
			if p.Urgency < 0 || p.Urgency > 7 {
				t.Fatalf("urgency out of bounds: %d", p.Urgency)
			}

			formatted := p.Format()
			if len(formatted) == 0 {
				t.Fatalf("expected non-empty formatted priority")
			}

			p2, err2 := priority.Parse(formatted)
			if err2 != nil || p2.Urgency != p.Urgency || p2.Incremental != p.Incremental {
				t.Fatalf("priority roundtrip mismatch: got %+v, expected %+v", p2, p)
			}
		}
	})
}
