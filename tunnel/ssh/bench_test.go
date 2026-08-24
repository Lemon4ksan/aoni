// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/require"
	"github.com/pkg/sftp"
	stdssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh"
)

type mockSSHServer struct {
	listener net.Listener
	addr     string
	port     uint
	hostKey  stdssh.Signer
	pubKey   stdssh.PublicKey
	user     string
	password string
	rootDir  string
	wg       sync.WaitGroup
}

func startMockServerTB(tb testing.TB) *mockSSHServer {
	tb.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(tb, err)

	signer, err := stdssh.NewSignerFromKey(priv)
	require.NoError(tb, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)

	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(tb, err)

	port, err := strconv.ParseUint(portStr, 10, 32)
	require.NoError(tb, err)

	sshPubKey, err := stdssh.NewPublicKey(pub)
	require.NoError(tb, err)

	srv := &mockSSHServer{
		listener: listener,
		addr:     host,
		port:     uint(port),
		hostKey:  signer,
		pubKey:   sshPubKey,
		user:     "testuser",
		password: "testpassword",
		rootDir:  tb.TempDir(),
	}

	config := &stdssh.ServerConfig{
		PasswordCallback: func(conn stdssh.ConnMetadata, password []byte) (*stdssh.Permissions, error) {
			if conn.User() == srv.user && string(password) == srv.password {
				return nil, nil
			}

			return nil, errors.New("auth failed")
		},
		PublicKeyCallback: func(conn stdssh.ConnMetadata, key stdssh.PublicKey) (*stdssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
				return nil, nil
			}

			return nil, errors.New("auth failed")
		},
	}
	config.AddHostKey(signer)

	srv.wg.Add(1)

	go srv.serve(config)

	tb.Cleanup(func() {
		_ = listener.Close()

		srv.wg.Wait()
	})

	return srv
}

func (s *mockSSHServer) serve(config *stdssh.ServerConfig) {
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

func (s *mockSSHServer) handleConn(c net.Conn, config *stdssh.ServerConfig) {
	sConn, chans, reqs, err := stdssh.NewServerConn(c, config)
	if err != nil {
		_ = c.Close()
		return
	}

	defer sConn.Close()

	go stdssh.DiscardRequests(reqs)

	for newChan := range chans {
		switch newChan.ChannelType() {
		case "session":
			ch, requests, err := newChan.Accept()
			if err != nil {
				continue
			}

			go s.handleSession(ch, requests)

		case "direct-tcpip":
			ch, requests, err := newChan.Accept()
			if err != nil {
				continue
			}

			go s.handleDirectTcpip(ch, requests, newChan.ExtraData())

		default:
			_ = newChan.Reject(stdssh.UnknownChannelType, "unknown channel type")
		}
	}
}

func (s *mockSSHServer) handleSession(ch stdssh.Channel, reqs <-chan *stdssh.Request) {
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "pty-req", "env":
			_ = req.Reply(true, nil)
		case "exec":
			_ = req.Reply(true, nil)

			var msg struct{ Command string }

			_ = stdssh.Unmarshal(req.Payload, &msg)

			s.executeCmd(ch, msg.Command)

			return

		case "subsystem":
			_ = req.Reply(true, nil)

			var msg struct{ Name string }

			_ = stdssh.Unmarshal(req.Payload, &msg)

			if msg.Name == "sftp" {
				server, err := sftp.NewServer(ch)
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

func (s *mockSSHServer) executeCmd(ch stdssh.Channel, cmd string) {
	switch {
	case strings.HasPrefix(cmd, "scp -t") || strings.HasPrefix(cmd, "scp -tr"):
		s.handleSCP(ch)
	case strings.Contains(cmd, "echo hello"):
		_, _ = ch.Write([]byte("hello\n"))
		s.sendExitStatus(ch, 0)
	case strings.Contains(cmd, "stream_test"):
		_, _ = ch.Write([]byte("line1\nline2\nline3\n"))
		s.sendExitStatus(ch, 0)
	case strings.Contains(cmd, "fail_cmd"):
		_, _ = ch.Write([]byte("command failed\n"))
		s.sendExitStatus(ch, 1)
	case strings.HasPrefix(cmd, "/bin/sh"):
		buf, _ := io.ReadAll(ch)
		if len(buf) > 0 {
			_, _ = ch.Write(buf)
		} else {
			_, _ = ch.Write([]byte("ok\n"))
		}

		s.sendExitStatus(ch, 0)

	default:
		_, _ = ch.Write([]byte("ok\n"))
		s.sendExitStatus(ch, 0)
	}
}

func (s *mockSSHServer) handleSCP(ch stdssh.Channel) {
	reader := bufio.NewReader(ch)

	line, err := reader.ReadString('\n')
	if err != nil {
		s.sendExitStatus(ch, 1)
		return
	}

	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 3 || !strings.HasPrefix(parts[0], "C") {
		s.sendExitStatus(ch, 1)
		return
	}

	size, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		s.sendExitStatus(ch, 1)
		return
	}

	fileName := parts[2]
	targetPath := filepath.Join(s.rootDir, fileName)

	out, err := os.Create(targetPath)
	if err != nil {
		s.sendExitStatus(ch, 1)
		return
	}

	defer out.Close()

	if size > 0 {
		_, err = io.CopyN(out, reader, size)
		if err != nil {
			s.sendExitStatus(ch, 1)
			return
		}
	}

	_, _ = reader.ReadByte() // Read null byte \x00

	s.sendExitStatus(ch, 0)
}

func (s *mockSSHServer) handleDirectTcpip(ch stdssh.Channel, reqs <-chan *stdssh.Request, extra []byte) {
	defer ch.Close()

	go stdssh.DiscardRequests(reqs)

	type directMsg struct {
		RAddr string
		RPort uint32
		LAddr string
		LPort uint32
	}

	var msg directMsg
	if err := stdssh.Unmarshal(extra, &msg); err != nil {
		return
	}

	dest := net.JoinHostPort(msg.RAddr, strconv.Itoa(int(msg.RPort)))

	targetConn, err := net.Dial("tcp", dest)
	if err != nil {
		return
	}

	defer targetConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() { defer wg.Done(); _, _ = io.Copy(ch, targetConn) }()
	go func() { defer wg.Done(); _, _ = io.Copy(targetConn, ch) }()

	wg.Wait()
}

func (s *mockSSHServer) sendExitStatus(ch stdssh.Channel, code uint32) {
	type exitMsg struct{ Status uint32 }

	_, _ = ch.SendRequest("exit-status", false, stdssh.Marshal(exitMsg{Status: code}))
}

func BenchmarkParseKey(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	pemBlock, err := stdssh.MarshalPrivateKey(priv, "bench-key")
	if err != nil {
		b.Fatal(err)
	}

	unencryptedBytes := pem.EncodeToMemory(pemBlock)

	encBlock, err := stdssh.MarshalPrivateKeyWithPassphrase(priv, "bench-enc", []byte("secretpass"))
	if err != nil {
		b.Fatal(err)
	}

	encryptedBytes := pem.EncodeToMemory(encBlock)

	b.Run("Unencrypted", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = ssh.ParseKey(unencryptedBytes, "")
		}
	})

	b.Run("EncryptedPassphrase", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = ssh.ParseKey(encryptedBytes, "secretpass")
		}
	})
}

func BenchmarkKnownHosts(b *testing.B) {
	tempDir := b.TempDir()
	knownFile := filepath.Join(tempDir, "known_hosts")

	_, err := ssh.EnsureKnownHosts(knownFile)
	if err != nil {
		b.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	signer, err := stdssh.NewSignerFromKey(priv)
	if err != nil {
		b.Fatal(err)
	}

	pubKey := signer.PublicKey()
	_ = pub

	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
	host := "127.0.0.1:2222"

	if err := ssh.AddKnownHost(host, addr, pubKey, knownFile); err != nil {
		b.Fatal(err)
	}

	b.Run("CheckKnownHost", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = ssh.CheckKnownHost(host, addr, pubKey, knownFile)
		}
	})
}

func BenchmarkClientOperations(b *testing.B) {
	srv := startMockServerTB(b)
	ctx := context.Background()

	client, err := ssh.NewClient(
		ctx,
		srv.user,
		srv.addr,
		ssh.WithPort(srv.port),
		ssh.WithPassword(srv.password),
		ssh.WithInsecureIgnoreHostKey(),
	)
	if err != nil {
		b.Fatal(err)
	}

	defer client.Close()

	b.Run("Command Execution", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = client.Run(b.Context(), "echo hello")
		}
	})

	b.Run("Stream Processing", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			outCh, errCh, doneCh, cmdErrCh, err := client.Stream(ctx, "stream_test", 5*time.Second)
			if err != nil {
				b.Fatal(err)
			}

			for range outCh {
			}

			for range errCh {
			}

			<-doneCh
			<-cmdErrCh
		}
	})

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
			_ = client.Scp(ctx, localUploadPath, remotePath)
		}
	})

	b.Run("SFTP Upload Sequential 1MB", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			remotePath := filepath.Join(srv.rootDir, "sftp_seq_bench.bin")
			_ = client.Upload(localUploadPath, remotePath)
		}
	})

	b.Run("SFTP Upload Parallel 1MB 4Workers", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			remotePath := filepath.Join(srv.rootDir, "sftp_par_bench.bin")
			_ = client.UploadParallel(ctx, localUploadPath, remotePath, 4, 64*1024)
		}
	})
}
