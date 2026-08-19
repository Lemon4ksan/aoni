// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pkgsftp "github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh"
	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

func TestE2E_ServerClient_PasswordAuth(t *testing.T) {
	t.Parallel()

	_, privHost, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	hostSigner, err := golangssh.NewSignerFromKey(privHost)
	require.NoError(t, err)

	srvHandler := func(s ssh.Session) {
		cmd := s.RawCommand()
		if strings.Contains(cmd, "hello_e2e") {
			_, _ = io.WriteString(s, "hello_e2e\n")
			_ = s.Exit(0)

			return
		}

		_, _ = io.WriteString(s, "default_ok\n")
		_ = s.Exit(0)
	}

	server, err := ssh.NewServer(
		"127.0.0.1:0",
		srvHandler,
		ssh.WithHostKeySigner(hostSigner),
		ssh.WithPasswordAuth(func(ctx ssh.Context, password string) bool {
			return ctx.User() == "e2e_user" && password == "e2e_pass"
		}),
	)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_ = server.Shutdown(ctx)
	})

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	client, err := ssh.NewClient(
		ctx,
		"e2e_user",
		"127.0.0.1:"+portStr,
		ssh.WithPassword("e2e_pass"),
		ssh.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)
	require.NotNil(t, client)

	t.Cleanup(func() { _ = client.Close() })

	out, err := client.Run(t.Context(), "echo hello_e2e")
	require.NoError(t, err)
	assert.Equal(t, "hello_e2e\n", string(out))
}

func TestE2E_CA_CertAuthentication(t *testing.T) {
	t.Parallel()

	caObj, _, err := ssh.GenerateCA()
	require.NoError(t, err)

	_, userPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	userSigner, err := golangssh.NewSignerFromKey(userPriv)
	require.NoError(t, err)

	userCert, err := caObj.IssueUserCert(userSigner.PublicKey(), "cert_user")
	require.NoError(t, err)

	certSigner, err := golangssh.NewCertSigner(userCert, userSigner)
	require.NoError(t, err)

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	hostSigner, err := golangssh.NewSignerFromKey(hostPriv)
	require.NoError(t, err)

	server, err := ssh.NewServer(
		"127.0.0.1:0",
		func(s ssh.Session) {
			_, _ = io.WriteString(s, "cert_auth_success\n")
			_ = s.Exit(0)
		},
		ssh.WithHostKeySigner(hostSigner),
		ssh.WithUserCAKeys(caObj.PublicKey()),
	)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())

	client, err := ssh.NewClient(
		t.Context(),
		"cert_user",
		"127.0.0.1:"+portStr,
		ssh.WithCertSigner(certSigner),
		ssh.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = client.Close() })

	out, err := client.Run(t.Context(), "check")
	require.NoError(t, err)
	assert.Equal(t, "cert_auth_success\n", string(out))
}

func TestE2E_SFTP_FileTransfers(t *testing.T) {
	t.Parallel()

	srvRootDir := t.TempDir()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	hostSigner, err := golangssh.NewSignerFromKey(hostPriv)
	require.NoError(t, err)

	server, err := ssh.NewServer(
		"127.0.0.1:0",
		nil,
		ssh.WithHostKeySigner(hostSigner),
		ssh.WithPasswordAuth(func(_ ssh.Context, _ string) bool { return true }),
		ssh.WithSubsystem("sftp", func(s ssh.Session) {
			sftpSrv, err := pkgsftp.NewServer(s)
			if err == nil {
				_ = sftpSrv.Serve()
				_ = sftpSrv.Close()
			}
		}),
	)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())

	client, err := ssh.NewClient(
		t.Context(),
		"sftp_user",
		"127.0.0.1:"+portStr,
		ssh.WithPassword("any"),
		ssh.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = client.Close() })

	localDir := t.TempDir()
	localUploadPath := filepath.Join(localDir, "local_file.txt")
	testData := []byte("e2e sftp content payload 12345")
	require.NoError(t, os.WriteFile(localUploadPath, testData, 0o644))

	remotePath := filepath.Join(srvRootDir, "remote_file.txt")
	err = client.Upload(localUploadPath, remotePath)
	require.NoError(t, err)

	localDownloadPath := filepath.Join(localDir, "downloaded_file.txt")
	err = client.Download(remotePath, localDownloadPath)
	require.NoError(t, err)

	readBack, err := os.ReadFile(localDownloadPath)
	require.NoError(t, err)
	assert.Equal(t, testData, readBack)
}

func TestTarpit_FacadeAliases(t *testing.T) {
	t.Parallel()

	c1, c2 := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())

	go ssh.TarpitTrap(ctx, c1, 10*time.Millisecond)

	buf := make([]byte, 128)
	n, err := c2.Read(buf)
	require.NoError(t, err)
	assert.Contains(t, string(buf[:n]), "SSH-2.0-AoniTarpit_")

	cancel()

	_ = c2.Close()
}

func TestClient_ListenSOCKS5_DynamicPortForwarding(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}))
	t.Cleanup(targetServer.Close)

	srv := startMockServerTB(t)
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

	ln, err := c.ListenSOCKS5(ctx, "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	proxyURL, err := url.Parse("socks5://" + ln.Addr().String())
	require.NoError(t, err)

	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}

	resp, err := httpClient.Get(targetServer.URL + "/test")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok\n", string(body))
}
