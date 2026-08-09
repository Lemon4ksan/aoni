// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sysnet_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/sysnet"
)

func TestTuneSocketConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	defer ln.Close()

	go func() {
		conn, errAccept := ln.Accept()
		if errAccept == nil {
			sysnet.TuneSocketConn(conn)
			conn.Close()
		}
	}()

	client, errDial := net.Dial("tcp", ln.Addr().String())
	assert.NoError(t, errDial)

	sysnet.TuneSocketConn(client)
	client.Close()
}
