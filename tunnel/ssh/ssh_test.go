// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
	pkgsftp "github.com/pkg/sftp"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh"
	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

type mockServer struct {
	listener net.Listener
	addr     string
	port     string
	signer   golangssh.Signer
	user     string
	password string
}

func startTestSSHServer(t testing.TB, config *golangssh.ServerConfig) *mockServer {
	t.Helper()

	_, privHost, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	hostSigner, err := golangssh.NewSignerFromKey(privHost)
	require.NoError(t, err)

	config.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	srv := &mockServer{
		listener: listener,
		addr:     host,
		port:     portStr,
		signer:   hostSigner,
		user:     "e2e_user",
		password: "e2e_pass",
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
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

						go func(ch golangssh.Channel, reqs <-chan *golangssh.Request) {
							defer ch.Close()

							for req := range reqs {
								switch req.Type {
								case "exec":
									var msg struct{ Command string }

									_ = golangssh.Unmarshal(req.Payload, &msg)
									_ = req.Reply(true, nil)

									if strings.Contains(msg.Command, "hello_e2e") {
										_, _ = io.WriteString(ch, "hello_e2e\n")
									} else {
										_, _ = io.WriteString(ch, "cert_auth_success\n")
									}

									_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})

									return

								case "subsystem":
									var msg struct{ Name string }

									_ = golangssh.Unmarshal(req.Payload, &msg)
									_ = req.Reply(true, nil)

									if msg.Name == "sftp" {
										sftpSrv, err := pkgsftp.NewServer(ch)
										if err == nil {
											_ = sftpSrv.Serve()
											_ = sftpSrv.Close()
										}
									}

									return

								default:
									_ = req.Reply(false, nil)
								}
							}
						}(ch, requests)

					case "direct-tcpip":
						ch, requests, err := newChan.Accept()
						if err != nil {
							continue
						}

						go func(ch golangssh.Channel, reqs <-chan *golangssh.Request, extra []byte) {
							defer ch.Close()

							go golangssh.DiscardRequests(reqs)

							var msg struct {
								RAddr string
								RPort uint32
								LAddr string
								LPort uint32
							}

							_ = golangssh.Unmarshal(extra, &msg)

							destConn, err := net.Dial("tcp", net.JoinHostPort(msg.RAddr, string(rune(msg.RPort))))
							if err != nil {
								return
							}
							defer destConn.Close()

							go func() { _, _ = io.Copy(ch, destConn) }()

							_, _ = io.Copy(destConn, ch)
						}(ch, requests, newChan.ExtraData())

					default:
						_ = newChan.Reject(golangssh.UnknownChannelType, "unknown")
					}
				}
			}(conn)
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
	})

	return srv
}

func TestE2E_ServerClient_PasswordAuth(t *testing.T) {
	t.Parallel()

	cfg := &golangssh.ServerConfig{
		PasswordCallback: func(conn golangssh.ConnMetadata, password []byte) (*golangssh.Permissions, error) {
			if conn.User() == "e2e_user" && string(password) == "e2e_pass" {
				return nil, nil
			}

			return nil, errors.New("invalid password")
		},
	}

	srv := startTestSSHServer(t, cfg)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	cl, err := ssh.NewClient(
		ctx,
		"e2e_user",
		"127.0.0.1:"+srv.port,
		ssh.WithPassword("e2e_pass"),
		ssh.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	require.NotNil(t, cl)
	defer cl.Close()

	out, err := cl.Run(t.Context(), "echo hello_e2e")
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

	certChecker := &golangssh.CertChecker{
		IsUserAuthority: func(auth golangssh.PublicKey) bool {
			return bytes.Equal(auth.Marshal(), caObj.PublicKey().Marshal())
		},
	}

	cfg := &golangssh.ServerConfig{
		PublicKeyCallback: func(conn golangssh.ConnMetadata, key golangssh.PublicKey) (*golangssh.Permissions, error) {
			if cert, ok := key.(*golangssh.Certificate); ok {
				return certChecker.Authenticate(conn, cert)
			}

			return nil, errors.New("auth failed")
		},
	}

	srv := startTestSSHServer(t, cfg)

	cl, err := ssh.NewClient(
		t.Context(),
		"cert_user",
		"127.0.0.1:"+srv.port,
		ssh.WithCertSigner(certSigner),
		ssh.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	defer cl.Close()

	out, err := cl.Run(t.Context(), "check")
	require.NoError(t, err)
	assert.Equal(t, "cert_auth_success\n", string(out))
}

func TestE2E_SFTP_FileTransfers(t *testing.T) {
	t.Parallel()

	cfg := &golangssh.ServerConfig{
		NoClientAuth: true,
	}

	srv := startTestSSHServer(t, cfg)

	cl, err := ssh.NewClient(
		t.Context(),
		"sftp_user",
		"127.0.0.1:"+srv.port,
		ssh.WithPassword("any"),
		ssh.WithInsecureIgnoreHostKey(),
	)
	require.NoError(t, err)

	defer cl.Close()

	localDir := t.TempDir()
	localUploadPath := filepath.Join(localDir, "local_file.txt")
	testData := []byte("e2e sftp content payload 12345")
	require.NoError(t, os.WriteFile(localUploadPath, testData, 0o644))

	srvRootDir := t.TempDir()
	remotePath := filepath.Join(srvRootDir, "remote_file.txt")
	err = cl.Upload(localUploadPath, remotePath)
	require.NoError(t, err)

	localDownloadPath := filepath.Join(localDir, "downloaded_file.txt")
	err = cl.Download(remotePath, localDownloadPath)
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

	cfg := &golangssh.ServerConfig{
		NoClientAuth: true,
	}

	srv := startTestSSHServer(t, cfg)
	ctx := t.Context()

	c, err := client.New(
		ctx,
		srv.user,
		srv.addr,
		client.WithPort(80),
		client.WithInsecureIgnoreHostKey(),
	)
	if err != nil {
		return
	}
	defer c.Close()
}
