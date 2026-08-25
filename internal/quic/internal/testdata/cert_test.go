// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testdata

import (
	"crypto/tls"
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestCertificates(t *testing.T) {
	ln, err := tls.Listen("tcp", "localhost:4433", GetTLSConfig())
	require.NoError(t, err)

	go func() {
		conn, err := ln.Accept()
		require.NoError(t, err)
		defer conn.Close()
		_, err = conn.Write([]byte("foobar"))
		require.NoError(t, err)
	}()

	conn, err := tls.Dial("tcp", "localhost:4433", &tls.Config{RootCAs: GetRootCA()})
	require.NoError(t, err)
	data, err := io.ReadAll(conn)
	require.NoError(t, err)
	require.Equal(t, "foobar", string(data))
}
