// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

type mockL2Device struct {
	hwAddr  net.HardwareAddr
	readBuf []byte
	written []byte
	mu      sync.Mutex
	closed  bool
}

func (m *mockL2Device) WriteFrame(frame []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.written = append(m.written, frame...)

	return len(frame), nil
}

func (m *mockL2Device) ReadFrame(buf []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.readBuf) == 0 {
		return 0, io.EOF
	}

	n := copy(buf, m.readBuf)
	m.readBuf = m.readBuf[n:]

	return n, nil
}

func (m *mockL2Device) HardwareAddr() net.HardwareAddr {
	return m.hwAddr
}

func (m *mockL2Device) MTU() int {
	return 1500
}

func (m *mockL2Device) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true

	return nil
}

type mockStackDriver struct {
	dialedAddr string
}

func (m *mockStackDriver) DialL4(_ context.Context, _, addr string, _ netdial.DialOptions) (net.Conn, error) {
	m.dialedAddr = addr

	server, client := net.Pipe()
	go func() {
		_ = server.Close()
	}()

	return client, nil
}

func TestNetworkPredicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		net    netdial.Network
		isTCP  bool
		isUDP  bool
		isUnix bool
		isIP   bool
	}{
		{net: netdial.NetworkTCP, isTCP: true},
		{net: netdial.NetworkTCP4, isTCP: true},
		{net: netdial.NetworkTCP6, isTCP: true},
		{net: netdial.NetworkUDP, isUDP: true},
		{net: netdial.NetworkUDP4, isUDP: true},
		{net: netdial.NetworkUDP6, isUDP: true},
		{net: netdial.NetworkUnix, isUnix: true},
		{net: netdial.NetworkUnixGram, isUnix: true},
		{net: netdial.NetworkUnixPacket, isUnix: true},
		{net: netdial.NetworkIP, isIP: true},
		{net: netdial.NetworkIP4, isIP: true},
		{net: netdial.NetworkIP6, isIP: true},
		{net: netdial.Network("unknown")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.net), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, string(tc.net), tc.net.String())
			assert.Equal(t, tc.isTCP, tc.net.IsTCP())
			assert.Equal(t, tc.isUDP, tc.net.IsUDP())
			assert.Equal(t, tc.isUnix, tc.net.IsUnix())
			assert.Equal(t, tc.isIP, tc.net.IsIP())
		})
	}
}

func TestL2DeviceAndConn_Edge(t *testing.T) {
	t.Parallel()

	t.Run("nil_l2_addr", func(t *testing.T) {
		t.Parallel()

		var nilAddr *netdial.L2Addr
		assert.Equal(t, "00:00:00:00:00:00", nilAddr.String())

		emptyAddr := &netdial.L2Addr{}
		assert.Equal(t, "00:00:00:00:00:00", emptyAddr.String())
		assert.Equal(t, "ethernet", emptyAddr.Network())
	})

	t.Run("nil_device_operations", func(t *testing.T) {
		t.Parallel()

		conn := netdial.NewL2FrameConn(nil, nil, nil)
		require.NotNil(t, conn)

		buf := make([]byte, 10)
		_, err := conn.Read(buf)
		assert.ErrorIs(t, err, netdial.ErrL2DeviceNil)

		_, err = conn.Write([]byte("test"))
		assert.ErrorIs(t, err, netdial.ErrL2DeviceNil)

		assert.NoError(t, conn.Close())
		assert.Equal(t, "00:00:00:00:00:00", conn.LocalAddr().String())
		assert.Equal(t, "00:00:00:00:00:00", conn.RemoteAddr().String())
		assert.NoError(t, conn.SetDeadline(time.Time{}))
		assert.NoError(t, conn.SetReadDeadline(time.Time{}))
		assert.NoError(t, conn.SetWriteDeadline(time.Time{}))
	})

	t.Run("mock_device_io", func(t *testing.T) {
		t.Parallel()

		mac, err := net.ParseMAC("aa:bb:cc:dd:ee:ff")
		require.NoError(t, err)

		remoteMAC, err := net.ParseMAC("11:22:33:44:55:66")
		require.NoError(t, err)

		mock := &mockL2Device{
			hwAddr:  mac,
			readBuf: []byte("frame_payload"),
		}

		conn := netdial.NewL2FrameConn(mock, nil, &netdial.L2Addr{HardwareAddr: remoteMAC})
		require.NotNil(t, conn)
		assert.Equal(t, "aa:bb:cc:dd:ee:ff", conn.LocalAddr().String())
		assert.Equal(t, "11:22:33:44:55:66", conn.RemoteAddr().String())

		buf := make([]byte, 20)
		n, err := conn.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "frame_payload", string(buf[:n]))

		wn, err := conn.Write([]byte("outgoing_frame"))
		require.NoError(t, err)
		assert.Equal(t, 14, wn)
		assert.Equal(t, "outgoing_frame", string(mock.written))

		assert.Equal(t, 1500, mock.MTU())
		assert.NoError(t, conn.Close())
		assert.True(t, mock.closed)
	})

	t.Run("dial_with_l2_device", func(t *testing.T) {
		t.Parallel()

		mock := &mockL2Device{}
		opts := netdial.DialOptions{
			L2Device: mock,
		}

		conn, err := netdial.DialL4(t.Context(), "tcp", "example.com:80", opts)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })

		assert.NotNil(t, conn)
	})

	t.Run("dial_with_stack_driver", func(t *testing.T) {
		t.Parallel()

		driver := &mockStackDriver{}
		opts := netdial.DialOptions{
			StackDriver: driver,
		}

		conn, err := netdial.DialL4(t.Context(), "tcp", "stack.local:8080", opts)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })

		assert.Equal(t, "stack.local:8080", driver.dialedAddr)
	})
}

func TestDialProxy_ErrorsAndHTTP(t *testing.T) {
	t.Parallel()

	t.Run("nil_or_empty_proxy_url", func(t *testing.T) {
		t.Parallel()
		_, err := netdial.DialProxy(t.Context(), nil, "host", "80", netdial.DialOptions{})
		assert.ErrorIs(t, err, netdial.ErrEmptyProxyURL)

		emptyURL, _ := url.Parse("")
		_, err = netdial.DialProxy(t.Context(), emptyURL, "host", "80", netdial.DialOptions{})
		assert.ErrorIs(t, err, netdial.ErrEmptyProxyURL)
	})

	t.Run("http_proxy_rejection", func(t *testing.T) {
		t.Parallel()

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = ln.Close() })

		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			br := bufio.NewReader(conn)
			_, _ = http.ReadRequest(br)
			_, _ = conn.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
		}()

		proxyURL, err := url.Parse("http://" + ln.Addr().String())
		require.NoError(t, err)

		_, err = netdial.DialProxy(t.Context(), proxyURL, "target.com", "443", netdial.DialOptions{})
		require.Error(t, err)
		assert.ErrorIs(t, err, netdial.ErrProxyConnectFailed)
	})

	t.Run("http_proxy_connect_success", func(t *testing.T) {
		t.Parallel()

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = ln.Close() })

		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			br := bufio.NewReader(conn)

			req, err := http.ReadRequest(br)
			if err != nil {
				return
			}

			if req.Method == http.MethodConnect {
				_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				// Echo payload back
				_, _ = io.Copy(conn, conn)
			}
		}()

		proxyURL, err := url.Parse("http://" + ln.Addr().String())
		require.NoError(t, err)

		conn, err := netdial.DialProxy(t.Context(), proxyURL, "target.com", "443", netdial.DialOptions{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })

		_, err = conn.Write([]byte("ping"))
		require.NoError(t, err)

		buf := make([]byte, 4)
		_, err = io.ReadFull(conn, buf)
		require.NoError(t, err)
		assert.Equal(t, "ping", string(buf))
	})
}

func TestCertificatePinning_Edge(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(200),
		Subject:      pkix.Name{CommonName: "edge.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	certParsed, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)

	spkiHash := sha256.Sum256(certParsed.RawSubjectPublicKeyInfo)
	base64Pin := base64.StdEncoding.EncodeToString(spkiHash[:])
	rawBase64Pin := base64.RawStdEncoding.EncodeToString(spkiHash[:])

	rawCerts := [][]byte{certDER}

	t.Run("raw_base64_pin", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"edge.example.com": {rawBase64Pin}}
		err := netdial.VerifyCertificatePins("edge.example.com", pins, rawCerts)
		assert.NoError(t, err)
	})

	t.Run("no_matching_domain_pin", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"other.com": {base64Pin}}
		err := netdial.VerifyCertificatePins("edge.example.com", pins, rawCerts)
		assert.NoError(t, err) // No pins configured for this domain means pass
	})

	t.Run("corrupt_cert_der", func(t *testing.T) {
		t.Parallel()

		pins := map[string][]string{"edge.example.com": {base64Pin}}
		err := netdial.VerifyCertificatePins("edge.example.com", pins, [][]byte{[]byte("invalid cert")})
		require.Error(t, err)
	})
}

func generateSelfSignedCert(t *testing.T, dnsName string) (tls.Certificate, []byte) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Aoni Test"}},
		DNSNames:     []string{dnsName},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}

	return tlsCert, certDER
}

func TestHandshakeUTLS_RealHandshake(t *testing.T) {
	t.Parallel()

	tlsCert, certDER := generateSelfSignedCert(t, "localhost")

	parsedCert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)

	spki := sha256.Sum256(parsedCert.RawSubjectPublicKeyInfo)
	pinBase64 := base64.StdEncoding.EncodeToString(spki[:])

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"http/1.1"},
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()

				buf := make([]byte, 1024)

				n, _ := c.Read(buf)
				if n > 0 {
					_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"))
				}
			}(conn)
		}
	}()

	t.Run("successful_utls_handshake_with_ja4", func(t *testing.T) {
		t.Parallel()

		rawConn, err := net.Dial("tcp", ln.Addr().String())
		require.NoError(t, err)
		t.Cleanup(func() { _ = rawConn.Close() })

		var ja4Report ja4.Report

		opts := netdial.RTLSOptions{
			InsecureSkipVerify: true,
			CertificatePins: map[string][]string{
				"localhost": {pinBase64},
			},
			CertCompression: []cert.CompressionAlgorithm{cert.CompressionBrotli},
			JA4Callback: func(r ja4.Report) {
				ja4Report = r
			},
		}

		uConn, report, err := netdial.HandshakeUTLS(t.Context(), rawConn, "localhost", opts)
		require.NoError(t, err)
		require.NotNil(t, uConn)
		t.Cleanup(func() { _ = uConn.Close() })

		assert.NotEmpty(t, report.JA4)
		assert.Equal(t, report.JA4, ja4Report.JA4)

		state := uConn.ConnectionState()
		assert.True(t, state.HandshakeComplete)
		assert.NotEmpty(t, state.PeerCertificates)

		// Verify roundtrip read/write
		_, err = uConn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
		require.NoError(t, err)

		respBuf := make([]byte, 256)
		n, err := uConn.Read(respBuf)
		require.NoError(t, err)
		assert.Contains(t, string(respBuf[:n]), "200 OK")
	})

	t.Run("pin_mismatch_fails_handshake", func(t *testing.T) {
		t.Parallel()

		rawConn, err := net.Dial("tcp", ln.Addr().String())
		require.NoError(t, err)
		t.Cleanup(func() { _ = rawConn.Close() })

		wrongPin := base64.StdEncoding.EncodeToString(make([]byte, 32))
		opts := netdial.RTLSOptions{
			InsecureSkipVerify: true,
			CertificatePins: map[string][]string{
				"localhost": {wrongPin},
			},
		}

		_, _, err = netdial.HandshakeUTLS(t.Context(), rawConn, "localhost", opts)
		require.Error(t, err)
		assert.ErrorIs(t, err, netdial.ErrUTLSHandshakeFailed)
	})
}
