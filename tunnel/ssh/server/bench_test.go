// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"testing"

	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/server"
)

func BenchmarkServer(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	signer, err := golangssh.NewSignerFromKey(priv)
	if err != nil {
		b.Fatal(err)
	}

	handler := func(s server.Session) {
		_, _ = io.WriteString(s, "ok\n")
		_ = s.Exit(0)
	}

	srv, err := server.New(
		"127.0.0.1:0",
		handler,
		server.WithHostKeySigner(signer),
		server.WithPasswordAuth(func(_ server.Context, password string) bool {
			return password == "secret"
		}),
	)
	if err != nil {
		b.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}

	go func() {
		_ = srv.Serve(listener)
	}()

	b.Cleanup(func() {
		_ = srv.Close()
	})

	clientCfg := &golangssh.ClientConfig{
		User: "bench",
		Auth: []golangssh.AuthMethod{
			golangssh.Password("secret"),
		},
		HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
	}

	b.Run("Handshake", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			c, err := golangssh.Dial("tcp", listener.Addr().String(), clientCfg)
			if err != nil {
				b.Fatal(err)
			}

			_ = c.Close()
		}
	})

	b.Run("ExecCommand", func(b *testing.B) {
		c, err := golangssh.Dial("tcp", listener.Addr().String(), clientCfg)
		if err != nil {
			b.Fatal(err)
		}

		defer c.Close()

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			sess, err := c.NewSession()
			if err != nil {
				b.Fatal(err)
			}

			_, _ = sess.CombinedOutput("echo test")
			_ = sess.Close()
		}
	})

	b.Run("ParallelExec", func(b *testing.B) {
		c, err := golangssh.Dial("tcp", listener.Addr().String(), clientCfg)
		if err != nil {
			b.Fatal(err)
		}

		defer c.Close()

		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				sess, err := c.NewSession()
				if err != nil {
					return
				}

				_, _ = sess.CombinedOutput("ping")
				_ = sess.Close()
			}
		})
	})
}
