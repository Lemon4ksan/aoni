// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sftp_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	pkgsftp "github.com/pkg/sftp"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
	aonisftp "github.com/lemon4ksan/aoni/tunnel/ssh/sftp"
)

type mockServer struct {
	listener net.Listener
	addr     string
	port     uint
	user     string
	password string
	rootDir  string
	wg       sync.WaitGroup
}

func startMockServer(t testing.TB) *mockServer {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := golangssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	port, _ := strconv.ParseUint(portStr, 10, 32)

	_ = pub

	srv := &mockServer{
		listener: listener,
		addr:     host,
		port:     uint(port),
		user:     "benchuser",
		password: "benchpassword",
		rootDir:  t.TempDir(),
	}

	config := &golangssh.ServerConfig{
		PasswordCallback: func(conn golangssh.ConnMetadata, password []byte) (*golangssh.Permissions, error) {
			if conn.User() == srv.user && string(password) == srv.password {
				return nil, nil
			}

			return nil, errors.New("auth failed")
		},
	}
	config.AddHostKey(signer)

	srv.wg.Add(1)

	go srv.serve(config)

	t.Cleanup(func() {
		_ = listener.Close()

		srv.wg.Wait()
	})

	return srv
}

func (s *mockServer) serve(config *golangssh.ServerConfig) {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		s.wg.Add(1)

		go func(c net.Conn) {
			defer s.wg.Done()

			s.handleConn(c, config)
		}(conn)
	}
}

func (s *mockServer) handleConn(c net.Conn, config *golangssh.ServerConfig) {
	sConn, chans, reqs, err := golangssh.NewServerConn(c, config)
	if err != nil {
		_ = c.Close()
		return
	}
	defer sConn.Close()

	go golangssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() == "session" {
			ch, requests, err := newChan.Accept()
			if err != nil {
				continue
			}

			go s.handleSession(ch, requests)
		} else {
			_ = newChan.Reject(golangssh.UnknownChannelType, "unknown channel")
		}
	}
}

func (s *mockServer) handleSession(ch golangssh.Channel, reqs <-chan *golangssh.Request) {
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "exec":
			_ = req.Reply(true, nil)
			_, _ = io.ReadAll(ch)

			type exitMsg struct{ Status uint32 }

			_, _ = ch.SendRequest("exit-status", false, golangssh.Marshal(exitMsg{Status: 0}))

			return

		case "subsystem":
			_ = req.Reply(true, nil)

			var msg struct{ Name string }

			_ = golangssh.Unmarshal(req.Payload, &msg)
			if msg.Name == "sftp" {
				server, err := pkgsftp.NewServer(ch)
				if err == nil {
					_ = server.Serve()
					_ = server.Close()
				}
			}

			return

		default:
			_ = req.Reply(false, nil)
		}
	}
}

func BenchmarkTransfers(b *testing.B) {
	srv := startMockServer(b)
	ctx := context.Background()

	c, err := client.New(
		ctx,
		srv.user,
		srv.addr,
		client.WithPort(srv.port),
		client.WithPassword(srv.password),
		client.WithInsecureIgnoreHostKey(),
	)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	tempDir := b.TempDir()
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1MB payload

	localUploadPath := filepath.Join(tempDir, "bench_1mb.bin")
	if err := os.WriteFile(localUploadPath, payload, 0o644); err != nil {
		b.Fatal(err)
	}

	b.Run("SCP Transfer 1MB", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			remotePath := filepath.Join(srv.rootDir, "scp_bench.bin")
			_ = aonisftp.Scp(ctx, c.Client, localUploadPath, remotePath)
		}
	})

	b.Run("SFTP Upload Sequential 1MB", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			remotePath := filepath.Join(srv.rootDir, "sftp_seq_bench.bin")
			_ = aonisftp.Upload(c.Client, localUploadPath, remotePath, c.MaxPacketSize)
		}
	})

	b.Run("SFTP Upload Parallel 1MB 4Workers", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			remotePath := filepath.Join(srv.rootDir, "sftp_par_bench.bin")
			_ = aonisftp.UploadParallel(ctx, c.Client, localUploadPath, remotePath, 4, 64*1024, c.MaxPacketSize)
		}
	})
}
