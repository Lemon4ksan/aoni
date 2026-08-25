// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sftp_test

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
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

	"github.com/lemon4ksan/aoni/tunnel/ssh/sftp"
)

// ============================================================================
// MOCK SSH & SFTP/SCP SERVER FOR ISOLATED TESTING
// ============================================================================

type mockSFTPServer struct {
	listener net.Listener
	addr     string
	signer   golangssh.Signer
	pubKey   golangssh.PublicKey
	rootDir  string
	wg       sync.WaitGroup
}

func startMockSFTPServer(t *testing.T) (*mockSFTPServer, *golangssh.Client) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := golangssh.NewSignerFromKey(priv)
	require.NoError(t, err)

	sshPubKey, err := golangssh.NewPublicKey(pub)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &mockSFTPServer{
		listener: listener,
		addr:     listener.Addr().String(),
		signer:   signer,
		pubKey:   sshPubKey,
		rootDir:  t.TempDir(),
	}

	config := &golangssh.ServerConfig{
		NoClientAuth: true,
	}
	config.AddHostKey(signer)

	srv.wg.Add(1)

	go srv.serve(config)

	t.Cleanup(func() {
		_ = listener.Close()

		srv.wg.Wait()
	})

	// Dial client to mock server
	clientConfig := &golangssh.ClientConfig{
		User:            "sftp_test_user",
		Auth:            []golangssh.AuthMethod{golangssh.Password("none")},
		HostKeyCallback: golangssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	rawConn, err := net.Dial("tcp", srv.addr)
	require.NoError(t, err)

	sshConn, chans, reqs, err := golangssh.NewClientConn(rawConn, srv.addr, clientConfig)
	require.NoError(t, err)

	sshClient := golangssh.NewClient(sshConn, chans, reqs)
	t.Cleanup(func() { _ = sshClient.Close() })

	return srv, sshClient
}

func (s *mockSFTPServer) serve(config *golangssh.ServerConfig) {
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

func (s *mockSFTPServer) handleConn(c net.Conn, config *golangssh.ServerConfig) {
	sConn, chans, reqs, err := golangssh.NewServerConn(c, config)
	if err != nil {
		_ = c.Close()
		return
	}

	defer sConn.Close()

	go golangssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(golangssh.UnknownChannelType, "unknown channel type")
			continue
		}

		ch, requests, err := newChan.Accept()
		if err != nil {
			continue
		}

		go s.handleSession(ch, requests)
	}
}

func (s *mockSFTPServer) handleSession(ch golangssh.Channel, reqs <-chan *golangssh.Request) {
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "exec":
			_ = req.Reply(true, nil)

			var msg struct{ Command string }

			_ = golangssh.Unmarshal(req.Payload, &msg)

			if strings.HasPrefix(msg.Command, "scp -t") || strings.HasPrefix(msg.Command, "scp -tr") {
				s.handleSCP(ch)
			}

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

func (s *mockSFTPServer) handleSCP(ch golangssh.Channel) {
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

func (s *mockSFTPServer) sendExitStatus(ch golangssh.Channel, code uint32) {
	type exitMsg struct{ Status uint32 }

	_, _ = ch.SendRequest("exit-status", false, golangssh.Marshal(exitMsg{Status: code}))
}

func TestSFTP_UploadAndDownload_Sequential(t *testing.T) {
	t.Parallel()

	srv, sshClient := startMockSFTPServer(t)

	localDir := t.TempDir()
	localUploadPath := filepath.Join(localDir, "upload.txt")
	testData := []byte("hello sftp transfer sequential test payload 12345")
	require.NoError(t, os.WriteFile(localUploadPath, testData, 0o644))

	// 1. Upload file via SFTP
	remotePath := filepath.Join(srv.rootDir, "remote_upload.txt")
	err := sftp.Upload(sshClient, localUploadPath, remotePath, 64*1024)
	require.NoError(t, err)

	// 2. Download file back via SFTP
	localDownloadPath := filepath.Join(localDir, "download.txt")
	err = sftp.Download(sshClient, remotePath, localDownloadPath, 64*1024)
	require.NoError(t, err)

	// 3. Verify file integrity
	readBack, err := os.ReadFile(localDownloadPath)
	require.NoError(t, err)
	assert.Equal(t, testData, readBack)
}

func TestSFTP_UploadAndDownload_Parallel(t *testing.T) {
	t.Parallel()

	srv, sshClient := startMockSFTPServer(t)
	ctx := t.Context()

	localDir := t.TempDir()

	// Prepare ~1.6MB test payload
	largeData := bytes.Repeat([]byte("0123456789abcdef"), 100*1024)
	largeLocalPath := filepath.Join(localDir, "large_upload.bin")
	require.NoError(t, os.WriteFile(largeLocalPath, largeData, 0o644))

	// 1. Parallel Upload
	remotePath := filepath.Join(srv.rootDir, "large_remote.bin")
	err := sftp.UploadParallel(ctx, sshClient, largeLocalPath, remotePath, 4, 64*1024, 64*1024)
	require.NoError(t, err)

	// 2. Parallel Download
	largeDownloadPath := filepath.Join(localDir, "large_download.bin")
	err = sftp.DownloadParallel(ctx, sshClient, remotePath, largeDownloadPath, 4, 64*1024, 64*1024)
	require.NoError(t, err)

	// 3. Verify file integrity
	readBack, err := os.ReadFile(largeDownloadPath)
	require.NoError(t, err)
	assert.Equal(t, largeData, readBack)
}

func TestSFTP_ParallelTransfer_InvalidChunkSize(t *testing.T) {
	t.Parallel()

	_, sshClient := startMockSFTPServer(t)
	ctx := t.Context()

	localDir := t.TempDir()
	localPath := filepath.Join(localDir, "test.txt")
	require.NoError(t, os.WriteFile(localPath, []byte("data"), 0o644))

	// Invalid chunk size <= 0 must return ErrInvalidChunkSize
	errUpload := sftp.UploadParallel(ctx, sshClient, localPath, "remote.txt", 4, 0, 64*1024)
	assert.ErrorIs(t, errUpload, sftp.ErrInvalidChunkSize)

	errDownload := sftp.DownloadParallel(ctx, sshClient, "remote.txt", localPath, 4, -1, 64*1024)
	assert.ErrorIs(t, errDownload, sftp.ErrInvalidChunkSize)
}

func TestSCP_WriteFileAndScp(t *testing.T) {
	t.Parallel()

	srv, sshClient := startMockSFTPServer(t)
	ctx := t.Context()

	localDir := t.TempDir()
	localFile := filepath.Join(localDir, "scp_local.txt")
	testData := []byte("scp stream transfer payload")
	require.NoError(t, os.WriteFile(localFile, testData, 0o644))

	t.Run("scp_file_upload", func(t *testing.T) {
		t.Parallel()

		remotePath := filepath.Join(srv.rootDir, "remote_scp.txt")
		err := sftp.Scp(ctx, sshClient, localFile, remotePath)
		require.NoError(t, err)

		savedData, err := os.ReadFile(remotePath)
		require.NoError(t, err)
		assert.Equal(t, testData, savedData)
	})

	t.Run("write_file_stream", func(t *testing.T) {
		t.Parallel()

		streamData := []byte("direct reader stream payload")
		remotePath := filepath.Join(srv.rootDir, "remote_stream.txt")

		err := sftp.WriteFile(ctx, sshClient, bytes.NewReader(streamData), int64(len(streamData)), remotePath)
		require.NoError(t, err)

		savedData, err := os.ReadFile(remotePath)
		require.NoError(t, err)
		assert.Equal(t, streamData, savedData)
	})
}

func TestSCP_ControlCharSanitization(t *testing.T) {
	t.Parallel()

	_, sshClient := startMockSFTPServer(t)
	ctx := t.Context()

	invalidPaths := []string{
		"bad\nfile.txt",
		"bad\rfile.txt",
		"bad\x00file.txt",
	}

	for _, badPath := range invalidPaths {
		t.Run(fmt.Sprintf("path_%q", badPath), func(t *testing.T) {
			t.Parallel()

			err := sftp.WriteFile(ctx, sshClient, strings.NewReader("test"), 4, badPath)
			assert.ErrorIs(t, err, sftp.ErrInvalidTargetFile)
		})
	}
}

func TestSFTP_ClosedClient_Errors(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// NewClient on nil sshClient
	_, errNew := sftp.NewClient(nil, 64*1024)
	assert.ErrorIs(t, errNew, sftp.ErrClientClosed)

	// WriteFile on nil sshClient
	errWrite := sftp.WriteFile(ctx, nil, strings.NewReader("data"), 4, "remote.txt")
	assert.ErrorIs(t, errWrite, sftp.ErrClientClosed)

	// Upload on nil sshClient with existing local file
	localPath := filepath.Join(t.TempDir(), "existing.txt")
	require.NoError(t, os.WriteFile(localPath, []byte("data"), 0o644))

	errUpload := sftp.Upload(nil, localPath, "remote.txt", 64*1024)
	assert.ErrorIs(t, errUpload, sftp.ErrClientClosed)

	// Download on nil sshClient
	errDownload := sftp.Download(nil, "remote.txt", localPath, 64*1024)
	assert.ErrorIs(t, errDownload, sftp.ErrClientClosed)
}
