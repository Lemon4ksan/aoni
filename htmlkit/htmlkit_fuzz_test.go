// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package htmlkit_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/htmlkit"
)

func FuzzHTMLUnescape(f *testing.F) {
	f.Add([]byte("&lt;div class=&quot;main&quot;&gt;Hello &amp; welcome!&lt;/div&gt;"))
	f.Add([]byte("&#39;&#x2F;&#x3c;&#x3e;"))
	f.Add([]byte("&amp;&amp;&amp;"))
	f.Add([]byte("&nonexistent;"))
	f.Add([]byte("no entities here"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, src []byte) {
		res := htmlkit.Unescape(src)
		_ = res
	})
}
