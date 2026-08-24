// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"bufio"
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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	pkgsftp "github.com/pkg/sftp"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
	aonisftp "github.com/lemon4ksan/aoni/tunnel/ssh/sftp"
)

type mockSSHServer struct {
	listener net.Listener
	addr     string
	port     uint
	hostKey  golangssh.Signer
	pubKey   golangssh.PublicKey
	user     string
	password string
	rootDir  string
	wg       sync.WaitGroup
}

func startMockServer(t testing.TB) *mockSSHServer {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := golangssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	port, err := strconv.ParseUint(portStr, 10, 32)
	require.NoError(t, err)

	sshPubKey, err := golangssh.NewPublicKey(pub)
	require.NoError(t, err)

	srv := &mockSSHServer{
		listener: listener,
		addr:     host,
		port:     uint(port),
		hostKey:  signer,
		pubKey:   sshPubKey,
		user:     "testuser",
		password: "testpassword",
		rootDir:  t.TempDir(),
	}

	config := &golangssh.ServerConfig{
		PasswordCallback: func(conn golangssh.ConnMetadata, password []byte) (*golangssh.Permissions, error) {
			if conn.User() == srv.user && string(password) == srv.password {
				return nil, nil
			}

			return nil, errors.New("auth failed")
		},
		PublicKeyCallback: func(conn golangssh.ConnMetadata, key golangssh.PublicKey) (*golangssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
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

func (s *mockSSHServer) serve(config *golangssh.ServerConfig) {
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

func (s *mockSSHServer) handleConn(c net.Conn, config *golangssh.ServerConfig) {
	sConn, chans, reqs, err := golangssh.NewServerConn(c, config)
	if err != nil {
		_ = c.Close()
		return
	}

	defer sConn.Close()

	go golangssh.DiscardRequests(reqs)

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
			_ = newChan.Reject(golangssh.UnknownChannelType, "unknown channel type")
		}
	}
}

func (s *mockSSHServer) handleSession(ch golangssh.Channel, reqs <-chan *golangssh.Request) {
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "pty-req", "env":
			_ = req.Reply(true, nil)
		case "exec":
			_ = req.Reply(true, nil)

			var msg struct{ Command string }

			_ = golangssh.Unmarshal(req.Payload, &msg)

			s.executeCmd(ch, msg.Command)

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

func (s *mockSSHServer) executeCmd(ch golangssh.Channel, cmd string) {
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

func (s *mockSSHServer) handleSCP(ch golangssh.Channel) {
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

func (s *mockSSHServer) handleDirectTcpip(ch golangssh.Channel, reqs <-chan *golangssh.Request, extra []byte) {
	defer ch.Close()

	go golangssh.DiscardRequests(reqs)

	type directMsg struct {
		RAddr string
		RPort uint32
		LAddr string
		LPort uint32
	}

	var msg directMsg
	if err := golangssh.Unmarshal(extra, &msg); err != nil {
		return
	}

	dest := net.JoinHostPort(msg.RAddr, strconv.Itoa(int(msg.RPort)))

	targetConn, err := net.Dial("tcp", dest)
	if err != nil {
		return
	}

	defer targetConn.Close()

	var wg sync.WaitGroup

	wg.Go(func() { _, _ = io.Copy(ch, targetConn) })
	wg.Go(func() { _, _ = io.Copy(targetConn, ch) })

	wg.Wait()
}

func (s *mockSSHServer) sendExitStatus(ch golangssh.Channel, code uint32) {
	type exitMsg struct{ Status uint32 }

	_, _ = ch.SendRequest("exit-status", false, golangssh.Marshal(exitMsg{Status: code}))
}

func TestClientConnectionAndAuth(t *testing.T) {
	t.Parallel()

	srv := startMockServer(t)

	t.Run("successful password connection", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		c, err := client.New(
			ctx,
			srv.user,
			srv.addr,
			client.WithPort(srv.port),
			client.WithPassword(srv.password),
			client.WithInsecureIgnoreHostKey(),
		)
		require.NoError(t, err)

		require.NotNil(t, c)

		defer c.Close()

		out, err := c.Run(t.Context(), "echo hello")
		require.NoError(t, err)
		assert.Equal(t, "hello\n", string(out))
	})

	t.Run("failed auth connection", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		c, err := client.New(
			ctx,
			srv.user,
			srv.addr,
			client.WithPort(srv.port),
			client.WithPassword("wrongpass"),
			client.WithInsecureIgnoreHostKey(),
		)
		require.Error(t, err)
		assert.ErrorIs(t, err, client.ErrSSHDialFailed)
		assert.Nil(t, c)
	})

	t.Run("signer auth connection", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		c, err := client.New(
			ctx,
			srv.user,
			srv.addr,
			client.WithPort(srv.port),
			client.WithSigner(srv.hostKey),
			client.WithInsecureIgnoreHostKey(),
		)
		require.NoError(t, err)

		require.NotNil(t, c)

		defer c.Close()
	})
}

func TestClientCommandExecution(t *testing.T) {
	t.Parallel()

	srv := startMockServer(t)
	ctx := t.Context()

	c, err := client.New(
		ctx,
		srv.user,
		srv.addr,
		client.WithPort(srv.port),
		client.WithPassword(srv.password),
		client.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	t.Run("RunContext", func(t *testing.T) {
		t.Parallel()

		out, err := c.Run(ctx, "echo hello")
		require.NoError(t, err)
		assert.Equal(t, "hello\n", string(out))
	})

	t.Run("Command CombinedOutput and Output", func(t *testing.T) {
		t.Parallel()

		cmd, err := c.Command(t.Context(), "echo", "hello")
		require.NoError(t, err)

		out, err := cmd.CombinedOutput()
		require.NoError(t, err)
		assert.Equal(t, "hello\n", string(out))

		cmd2, err := c.Command(t.Context(), "echo", "hello")
		require.NoError(t, err)

		out2, err := cmd2.Output()
		require.NoError(t, err)
		assert.Equal(t, "hello\n", string(out2))
	})

	t.Run("Command Run and Start", func(t *testing.T) {
		t.Parallel()

		cmd, err := c.Command(t.Context(), "echo", "hello")
		require.NoError(t, err)
		require.NoError(t, cmd.Run())

		cmd2, err := c.Command(t.Context(), "echo", "hello")
		require.NoError(t, err)
		require.NoError(t, cmd2.Start())
	})

	t.Run("Command String", func(t *testing.T) {
		t.Parallel()

		cmd, err := c.Command(t.Context(), "ls", "-la", "/tmp")
		require.NoError(t, err)
		assert.Equal(t, "ls -la /tmp", cmd.String())
	})
}

func TestScriptExecution(t *testing.T) {
	t.Parallel()

	srv := startMockServer(t)
	ctx := t.Context()

	c, err := client.New(
		ctx,
		srv.user,
		srv.addr,
		client.WithPort(srv.port),
		client.WithPassword(srv.password),
		client.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	t.Run("Script reader", func(t *testing.T) {
		t.Parallel()

		scriptContent := strings.NewReader("echo hello\n")
		cmd, err := c.Script(ctx, scriptContent)
		require.NoError(t, err)

		out, err := cmd.CombinedOutput()
		require.NoError(t, err)
		assert.NotEmpty(t, out)
	})

	t.Run("ScriptFile", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		scriptPath := filepath.Join(tempDir, "script.sh")
		require.NoError(t, os.WriteFile(scriptPath, []byte("echo hello\n"), 0o755))

		cmd, err := c.ScriptFile(ctx, scriptPath)
		require.NoError(t, err)

		out, err := cmd.CombinedOutput()
		require.NoError(t, err)
		assert.NotEmpty(t, out)
	})
}

func TestStream(t *testing.T) {
	t.Parallel()

	srv := startMockServer(t)
	ctx := t.Context()

	c, err := client.New(
		ctx,
		srv.user,
		srv.addr,
		client.WithPort(srv.port),
		client.WithPassword(srv.password),
		client.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	outCh, errCh, doneCh, cmdErrCh, err := c.Stream(ctx, "stream_test", 5*time.Second)
	require.NoError(t, err)

	var lines []string
	for line := range outCh {
		lines = append(lines, line)
	}

	for range errCh {
	}

	done := <-doneCh
	assert.True(t, done)

	cmdErr := <-cmdErrCh
	assert.NoError(t, cmdErr)

	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
}

func TestSFTPTransfers(t *testing.T) {
	t.Parallel()

	srv := startMockServer(t)
	ctx := t.Context()

	c, err := client.New(
		ctx,
		srv.user,
		srv.addr,
		client.WithPort(srv.port),
		client.WithPassword(srv.password),
		client.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	localDir := t.TempDir()
	localUpload := filepath.Join(localDir, "upload.txt")
	testData := []byte("hello sftp transfer data test 1234567890")
	require.NoError(t, os.WriteFile(localUpload, testData, 0o644))

	t.Run("Upload and Download SFTP", func(t *testing.T) {
		t.Parallel()

		remotePath := filepath.Join(srv.rootDir, "remote_upload.txt")
		err := c.Upload(localUpload, remotePath)
		require.NoError(t, err)

		localDownload := filepath.Join(localDir, "download.txt")
		err = c.Download(remotePath, localDownload)
		require.NoError(t, err)

		readBack, err := os.ReadFile(localDownload)
		require.NoError(t, err)
		assert.Equal(t, testData, readBack)
	})

	t.Run("UploadParallel and DownloadParallel SFTP", func(t *testing.T) {
		t.Parallel()

		largeData := bytes.Repeat([]byte("0123456789abcdef"), 100*1024)
		largeLocalPath := filepath.Join(localDir, "large_upload.bin")
		require.NoError(t, os.WriteFile(largeLocalPath, largeData, 0o644))

		remotePath := filepath.Join(srv.rootDir, "large_remote.bin")
		err := c.UploadParallel(ctx, largeLocalPath, remotePath, 4, 64*1024)
		require.NoError(t, err)

		largeDownloadPath := filepath.Join(localDir, "large_download.bin")
		err = c.DownloadParallel(ctx, remotePath, largeDownloadPath, 4, 64*1024)
		require.NoError(t, err)

		readBack, err := os.ReadFile(largeDownloadPath)
		require.NoError(t, err)
		assert.Equal(t, largeData, readBack)
	})

	t.Run("Parallel transfers invalid chunk size", func(t *testing.T) {
		t.Parallel()

		err := c.UploadParallel(ctx, localUpload, "err.bin", 4, 0)
		assert.ErrorIs(t, err, aonisftp.ErrInvalidChunkSize)

		err = c.DownloadParallel(ctx, "err.bin", localUpload, 4, -1)
		assert.ErrorIs(t, err, aonisftp.ErrInvalidChunkSize)
	})
}

func TestSCPTransfers(t *testing.T) {
	t.Parallel()

	srv := startMockServer(t)
	ctx := t.Context()

	c, err := client.New(
		ctx,
		srv.user,
		srv.addr,
		client.WithPort(srv.port),
		client.WithPassword(srv.password),
		client.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	localDir := t.TempDir()
	localFile := filepath.Join(localDir, "scp_file.txt")
	testData := []byte("hello scp transfer")
	require.NoError(t, os.WriteFile(localFile, testData, 0o644))

	t.Run("Scp upload", func(t *testing.T) {
		t.Parallel()

		remotePath := filepath.Join(srv.rootDir, "remote_scp.txt")
		err := c.Scp(ctx, localFile, remotePath)
		require.NoError(t, err)

		savedData, err := os.ReadFile(remotePath)
		require.NoError(t, err)
		assert.Equal(t, testData, savedData)
	})

	t.Run("WriteFile invalid target file path", func(t *testing.T) {
		t.Parallel()

		err := c.WriteFile(ctx, strings.NewReader("test"), 4, "bad\nfile.txt")
		assert.ErrorIs(t, err, aonisftp.ErrInvalidTargetFile)
	})
}

func TestErrorConditions(t *testing.T) {
	t.Parallel()

	t.Run("ErrProxyAndJumpConflict", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		jumpClient := &client.Client{}

		_, err := client.New(
			ctx,
			"user",
			"127.0.0.1",
			client.WithJump(jumpClient),
			client.WithProxy("socks5://127.0.0.1:1080"),
		)
		assert.ErrorIs(t, err, client.ErrProxyAndJumpConflict)
	})

	t.Run("Operations on closed client", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c := &client.Client{}
		require.NoError(t, c.Close())

		_, err := c.Run(t.Context(), "echo hi")
		assert.ErrorIs(t, err, client.ErrSSHClosed)

		_, err = c.Command(t.Context(), "echo")
		assert.ErrorIs(t, err, client.ErrSSHClosed)

		_, err = c.NewSftp()
		assert.ErrorIs(t, err, client.ErrSSHClosed)

		err = c.WriteFile(ctx, strings.NewReader("test"), 4, "file.txt")
		assert.ErrorIs(t, err, client.ErrSSHClosed)

		_, err = c.DialContext(ctx, "tcp", "127.0.0.1:80")
		assert.ErrorIs(t, err, client.ErrSSHClosed)
	})
}

func TestSSHConfig_Parser(t *testing.T) {
	t.Parallel()

	configText := `
# OpenSSH test config
Host prod-db
    HostName db.company.internal
    User admin
    Port 2222
    StrictHostKeyChecking no
    UserKnownHostsFile /tmp/known_hosts

Host *
    User defaultuser
    Port 22
`

	cfg, err := client.ParseSSHConfig(strings.NewReader(configText))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Host prod-db
	prodHost := cfg.GetHost("prod-db")
	require.NotNil(t, prodHost)
	assert.Equal(t, "db.company.internal", prodHost.HostName)
	assert.Equal(t, "admin", prodHost.User)
	assert.Equal(t, uint(2222), prodHost.Port)
	assert.Equal(t, "no", prodHost.StrictHostKeyChecking)

	// Wildcard fallback host
	unknownHost := cfg.GetHost("unknown-host")
	require.NotNil(t, unknownHost)
	assert.Equal(t, "unknown-host", unknownHost.HostName)
	assert.Equal(t, "defaultuser", unknownHost.User)
	assert.Equal(t, uint(22), unknownHost.Port)
}
