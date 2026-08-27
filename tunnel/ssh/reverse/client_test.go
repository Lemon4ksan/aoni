// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reverse_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/require"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/reverse"
)

func TestExposeLocal_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := reverse.ExposeLocal(ctx, reverse.ClientConfig{
		ServerAddr: "127.0.0.1:0",
		LocalAddr:  "127.0.0.1:0",
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestExposeLocal_WithMockServer(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := golangssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	config := &golangssh.ServerConfig{
		NoClientAuth: true,
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	forwardReqReceived := make(chan struct{}, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		sConn, chans, reqs, err := golangssh.NewServerConn(conn, config)
		if err != nil {
			return
		}
		defer sConn.Close()

		go func() {
			for ch := range chans {
				_ = ch.Reject(golangssh.Prohibited, "prohibited")
			}
		}()

		for req := range reqs {
			if req.Type == "tcpip-forward" {
				_ = req.Reply(true, golangssh.Marshal(struct{ Port uint32 }{Port: 80}))

				select {
				case forwardReqReceived <- struct{}{}:
				default:
				}
			} else {
				_ = req.Reply(false, nil)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	go func() {
		_ = reverse.ExposeLocal(ctx, reverse.ClientConfig{
			ServerAddr: listener.Addr().String(),
			LocalAddr:  "127.0.0.1:8080",
			Subdomain:  "test-subdomain",
			SSHConfig: &golangssh.ClientConfig{
				User:            "aoni",
				HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
			},
		})
	}()

	select {
	case <-forwardReqReceived:
		// Success: client connected and sent tcpip-forward request
	case <-ctx.Done():
		t.Fatal("timed out waiting for reverse client tcpip-forward request")
	}
}
