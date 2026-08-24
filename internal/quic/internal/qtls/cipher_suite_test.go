// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qtls

import (
	"crypto/tls"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/testdata"
)

func TestCipherSuiteSelection(t *testing.T) {
	reset := SetCipherSuite(tls.TLS_AES_128_GCM_SHA256)
	require.NotNil(t, reset)
	reset()

	ln, err := tls.Listen("tcp4", "localhost:0", testdata.GetTLSConfig())
	require.NoError(t, err)

	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)

		conn, err := ln.Accept()
		require.NoError(t, err)
		_, err = conn.Read(make([]byte, 10))
		require.NoError(t, err)
		require.NotZero(t, conn.(*tls.Conn).ConnectionState().CipherSuite)
	}()

	conn, err := tls.Dial(
		"tcp4",
		fmt.Sprintf("localhost:%d", ln.Addr().(*net.TCPAddr).Port),
		&tls.Config{RootCAs: testdata.GetRootCA()},
	)
	require.NoError(t, err)
	_, err = conn.Write([]byte("foobar"))
	require.NoError(t, err)
	require.NotZero(t, conn.ConnectionState().CipherSuite)

	<-done
}
