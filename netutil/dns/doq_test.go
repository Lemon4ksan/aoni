// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/foundation/net/dns/wire"
)

func generateDoQTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"DoQ Test Org"}},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(1 * time.Hour),
		DNSNames:     []string{"localhost", "dns.test"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{DoQALPN},
	}
}

func startMockDoQServer(t *testing.T, handler func(stream *quic.Stream)) (string, *tls.Config, func()) {
	t.Helper()

	serverTLS := generateDoQTLSConfig(t)
	listener, err := quic.ListenAddr("127.0.0.1:0", serverTLS, &quic.Config{
		EnableDatagrams: true,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			conn, err := listener.Accept(ctx)
			if err != nil {
				return
			}

			go func(c *quic.Conn) {
				for {
					stream, err := c.AcceptStream(ctx)
					if err != nil {
						return
					}

					go handler(stream)
				}
			}(conn)
		}
	}()

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{DoQALPN},
	}

	cleanup := func() {
		cancel()

		_ = listener.Close()
	}

	return listener.Addr().String(), clientTLS, cleanup
}

func defaultMockDoQStreamHandler(stream *quic.Stream) {
	defer stream.Close()

	var lenBuf [2]byte
	if _, err := io.ReadFull(stream, lenBuf[:]); err != nil {
		return
	}

	msgLen := binary.BigEndian.Uint16(lenBuf[:])
	if msgLen == 0 {
		return
	}

	reqBuf := make([]byte, msgLen)
	if _, err := io.ReadFull(stream, reqBuf); err != nil {
		return
	}

	respIP := netip.MustParseAddr("127.0.0.1")

	// Parse QTYPE by skipping domain name in question section (offset 12)
	qtypeOffset, err := wire.SkipDomainName(reqBuf, 12)
	if err == nil && qtypeOffset+2 <= len(reqBuf) {
		qtype := binary.BigEndian.Uint16(reqBuf[qtypeOffset : qtypeOffset+2])
		if qtype == wire.TypeAAAA {
			respIP = netip.MustParseAddr("::1")
		}
	}

	respMsg := buildMockDoQDNSResponse(0, respIP)

	var respLenBuf [2]byte
	binary.BigEndian.PutUint16(respLenBuf[:], uint16(len(respMsg)))
	_, _ = stream.Write(respLenBuf[:])
	_, _ = stream.Write(respMsg)
}

func buildMockDoQDNSResponse(id uint16, ip netip.Addr) []byte {
	var buf bytes.Buffer

	var hdr [12]byte
	binary.BigEndian.PutUint16(hdr[0:2], id)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8100) // Response, No Error
	binary.BigEndian.PutUint16(hdr[4:6], 1)      // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], 1)      // ANCOUNT
	buf.Write(hdr[:])

	// Question Section
	buf.Write([]byte{4, 't', 'e', 's', 't', 0})

	var qTail [4]byte
	if ip.Is4() {
		binary.BigEndian.PutUint16(qTail[0:2], wire.TypeA)
	} else {
		binary.BigEndian.PutUint16(qTail[0:2], wire.TypeAAAA)
	}

	binary.BigEndian.PutUint16(qTail[2:4], wire.ClassIN)
	buf.Write(qTail[:])

	// Answer Section
	buf.Write([]byte{0xc0, 0x0c}) // Name pointer

	var ansHdr [10]byte
	if ip.Is4() {
		binary.BigEndian.PutUint16(ansHdr[0:2], wire.TypeA)
		binary.BigEndian.PutUint16(ansHdr[8:10], 4)
	} else {
		binary.BigEndian.PutUint16(ansHdr[0:2], wire.TypeAAAA)
		binary.BigEndian.PutUint16(ansHdr[8:10], 16)
	}

	binary.BigEndian.PutUint16(ansHdr[2:4], wire.ClassIN)
	binary.BigEndian.PutUint32(ansHdr[4:8], 120) // TTL = 120s
	buf.Write(ansHdr[:])

	rawIP := ip.AsSlice()
	buf.Write(rawIP)

	return buf.Bytes()
}

func TestNewDoQResolver(t *testing.T) {
	t.Parallel()

	resolver := NewDoQResolver("1.1.1.1:853", "dns.google")
	require.NotNil(t, resolver)

	assert.Equal(t, "1.1.1.1:853", resolver.Endpoint)
	assert.Equal(t, "dns.google", resolver.Host)
	assert.Equal(t, 5*time.Second, resolver.Timeout)
	assert.Equal(t, 128, resolver.EDNS.PadToBlock)
}

func TestHasPort(t *testing.T) {
	t.Parallel()

	assert.True(t, hasPort("1.1.1.1:853"))
	assert.True(t, hasPort("dns.google:853"))
	assert.True(t, hasPort("[::1]:853"))

	assert.False(t, hasPort("1.1.1.1"))
	assert.False(t, hasPort("dns.google"))
	assert.False(t, hasPort("::1"))
}

func TestDoQResolver_BuildTLSConfig(t *testing.T) {
	t.Parallel()

	t.Run("default_tls_config", func(t *testing.T) {
		t.Parallel()

		r := NewDoQResolver("1.1.1.1:853", "cloudflare-dns.com")
		tlsCfg := r.buildTLSConfig()

		require.NotNil(t, tlsCfg)
		assert.Equal(t, "cloudflare-dns.com", tlsCfg.ServerName)
		assert.Equal(t, []string{DoQALPN}, tlsCfg.NextProtos)
		assert.Equal(t, uint16(tls.VersionTLS13), tlsCfg.MinVersion)
	})

	t.Run("custom_tls_config_override", func(t *testing.T) {
		t.Parallel()

		r := NewDoQResolver("1.1.1.1", "")
		r.TLSConfig = &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         "custom.host",
		}

		tlsCfg := r.buildTLSConfig()
		require.NotNil(t, tlsCfg)
		assert.True(t, tlsCfg.InsecureSkipVerify)
		assert.Equal(t, "custom.host", tlsCfg.ServerName)
		assert.Equal(t, []string{DoQALPN}, tlsCfg.NextProtos)
	})
}

func TestDoQResolver_LookupIPAddr_Success(t *testing.T) {
	t.Parallel()

	addrStr, clientTLS, cleanup := startMockDoQServer(t, defaultMockDoQStreamHandler)
	t.Cleanup(cleanup)

	resolver := NewDoQResolver(addrStr, "localhost")
	resolver.TLSConfig = clientTLS

	ctx := t.Context()

	// Test LookupDNSRecords
	records, err := resolver.LookupDNSRecords(ctx, "example.com")
	require.NoError(t, err)
	require.Len(t, records, 2)

	var hasV4, hasV6 bool
	for _, rec := range records {
		assert.Equal(t, uint32(120), rec.TTL)

		if rec.Addr.Is4() {
			assert.Equal(t, "127.0.0.1", rec.Addr.String())

			hasV4 = true
		} else if rec.Addr.Is6() {
			assert.Equal(t, "::1", rec.Addr.String())

			hasV6 = true
		}
	}

	assert.True(t, hasV4)
	assert.True(t, hasV6)

	// Test LookupIPAddr
	ipAddrs, err := resolver.LookupIPAddr(ctx, "example.com")
	require.NoError(t, err)
	require.Len(t, ipAddrs, 2)
}

func TestDoQResolver_HandshakeFailure(t *testing.T) {
	t.Parallel()

	// Connect to an unreachable address to trigger handshake failure
	resolver := NewDoQResolver("127.0.0.1:1", "localhost")
	resolver.TLSConfig = &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{DoQALPN},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := resolver.LookupIPAddr(ctx, "example.com")
	require.Error(t, err)

	var resErr *ResolutionError
	require.ErrorAs(t, err, &resErr)
	assert.Equal(t, "DoQ", resErr.Resolver)
	assert.ErrorIs(t, resErr.Err, ErrDoQHandshakeFailed)
}

func TestDoQResolver_InvalidMessageResponse(t *testing.T) {
	t.Parallel()

	addrStr, clientTLS, cleanup := startMockDoQServer(t, func(stream *quic.Stream) {
		defer stream.Close()

		// Send 2-byte length of 0
		var zeroLen [2]byte

		_, _ = stream.Write(zeroLen[:])
	})
	t.Cleanup(cleanup)

	resolver := NewDoQResolver(addrStr, "localhost")
	resolver.TLSConfig = clientTLS

	_, err := resolver.LookupDNSRecords(t.Context(), "example.com")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDoQInvalidMessage)
}

func TestDoQResolver_StreamClosedPrematurely(t *testing.T) {
	t.Parallel()

	addrStr, clientTLS, cleanup := startMockDoQServer(t, func(stream *quic.Stream) {
		// Close stream immediately without responding
		_ = stream.Close()
	})
	t.Cleanup(cleanup)

	resolver := NewDoQResolver(addrStr, "localhost")
	resolver.TLSConfig = clientTLS

	_, err := resolver.LookupDNSRecords(t.Context(), "example.com")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDoQStreamClosed)
}

func TestDoQResolver_CloseConn(t *testing.T) {
	t.Parallel()

	addrStr, clientTLS, cleanup := startMockDoQServer(t, defaultMockDoQStreamHandler)
	t.Cleanup(cleanup)

	resolver := NewDoQResolver(addrStr, "localhost")
	resolver.TLSConfig = clientTLS

	ctx := t.Context()

	// Initial connection setup
	_, err := resolver.getOrCreateConn(ctx)
	require.NoError(t, err)
	assert.NotNil(t, resolver.conn)

	// Explicit close
	resolver.closeConn()
	assert.Nil(t, resolver.conn)
}
