// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h3engine_test

import (
	"crypto/tls"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/fast/h3engine"
	"github.com/lemon4ksan/aoni/internal/quic"
)

func TestH3Client_InitializationAndClose(t *testing.T) {
	t.Parallel()

	tlsCfg := &tls.Config{InsecureSkipVerify: true}
	quicCfg := &quic.Config{EnableDatagrams: true}

	client := h3engine.NewClient(tlsCfg, quicCfg)
	require.NotNil(t, client)
	assert.Contains(t, client.TLSConfig.NextProtos, "h3")

	err := client.Close()
	assert.NoError(t, err)
}
