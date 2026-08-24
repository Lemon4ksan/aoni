// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inspector_test

import (
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/inspector"
)

func TestMultiInspector_IsolatedBroadcasting(t *testing.T) {
	t.Parallel()

	t.Run("empty_broadcaster_does_not_panic", func(t *testing.T) {
		t.Parallel()

		multi := inspector.NewMultiInspector()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
		resp := &http.Response{StatusCode: 200}

		assert.NotPanics(t, func() {
			multi.Capture(req, resp, nil, nil)
		})
	})

	t.Run("dynamic_add_and_fanout", func(t *testing.T) {
		t.Parallel()

		insp1 := inspector.NewTrafficInspector("127.0.0.1:0")
		insp2 := inspector.NewTrafficInspector("127.0.0.1:0")

		multi := inspector.NewMultiInspector(insp1).Add(insp2)

		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/dynamic", nil)
		resp := &http.Response{StatusCode: 200}

		multi.Capture(req, resp, nil, nil)

		assert.Len(t, insp1.GetRequests(), 1)
		assert.Len(t, insp2.GetRequests(), 1)
	})
}
