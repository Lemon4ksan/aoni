// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/net/tls/cert"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	"github.com/lemon4ksan/mach/proto/dns/wire"

	"github.com/lemon4ksan/aoni/dns"
)

func generateTestCert(t *testing.T, dnsNames ...string) (tls.Certificate, string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	if len(dnsNames) == 0 {
		dnsNames = []string{"localhost", "dot.local", "dns.example.com"}
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              dnsNames,
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	parsedCert, err := x509.ParseCertificate(derBytes)
	require.NoError(t, err)

	spkiPin, err := cert.SPKIFingerprintBase64(parsedCert)
	require.NoError(t, err)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}

	return tlsCert, spkiPin
}

func startMockDoTServer(t *testing.T, tlsCert tls.Certificate) (string, func()) {
	t.Helper()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	require.NoError(t, err)

	stop := make(chan struct{})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-stop:
					return
				default:
					return
				}
			}

			go handleDoTConn(conn)
		}
	}()

	cleanup := func() {
		close(stop)

		_ = listener.Close()
	}

	return listener.Addr().String(), cleanup
}

func handleDoTConn(conn net.Conn) {
	defer conn.Close()

	for {
		var lenBuf [2]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}

		reqLen := int(binary.BigEndian.Uint16(lenBuf[:]))

		reqBuf := make([]byte, reqLen)
		if _, err := io.ReadFull(conn, reqBuf); err != nil {
			return
		}

		if len(reqBuf) < 12 {
			return
		}

		txID := binary.BigEndian.Uint16(reqBuf[0:2])

		// Craft response: Question + Answer (127.0.0.1)
		resp := make([]byte, 0, 128)

		var h [12]byte
		binary.BigEndian.PutUint16(h[0:2], txID)
		binary.BigEndian.PutUint16(h[2:4], 0x8180) // NOERROR
		binary.BigEndian.PutUint16(h[4:6], 1)      // QDCOUNT = 1
		binary.BigEndian.PutUint16(h[6:8], 1)      // ANCOUNT = 1
		resp = append(resp, h[:]...)

		// Echo question from request
		qEnd, err := wire.SkipDomainName(reqBuf, 12)
		if err != nil || qEnd+4 > len(reqBuf) {
			return
		}

		resp = append(resp, reqBuf[12:qEnd+4]...)

		// Answer record (A: 127.0.0.1 / TTL 300)
		resp = append(resp, 0xc0, 0x0c) // Compression pointer to question

		var anH [10]byte
		binary.BigEndian.PutUint16(anH[0:2], wire.TypeA)
		binary.BigEndian.PutUint16(anH[2:4], wire.ClassIN)
		binary.BigEndian.PutUint32(anH[4:8], 300)
		binary.BigEndian.PutUint16(anH[8:10], 4)
		resp = append(resp, anH[:]...)
		resp = append(resp, 127, 0, 0, 1)

		// Send 2-octet length + response
		var respLenBuf [2]byte
		binary.BigEndian.PutUint16(respLenBuf[:], uint16(len(resp)))

		if _, err := conn.Write(append(respLenBuf[:], resp...)); err != nil {
			return
		}
	}
}

func TestRFC7858_RFC8310_DoTProfilesAndADN(t *testing.T) {
	t.Parallel()

	tlsCert, validPin := generateTestCert(t, "dns.example.com", "localhost")

	serverAddr, cleanup := startMockDoTServer(t, tlsCert)
	defer cleanup()

	// 1. Opportunistic Profile (RFC 8310 §5 & §6.5)
	resolver := dns.NewDoTResolver(serverAddr, "localhost").
		WithPrivacyProfile(dns.PrivacyProfileOpportunistic)
	resolver.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	ips, err := resolver.LookupIPAddr(t.Context(), "example.com")
	require.NoError(t, err)
	require.NotEmpty(t, ips)
	assert.Equal(t, "127.0.0.1", ips[0].IP.String())

	// 2. Strict Privacy Profile with matching ADN (RFC 8310 §6.6 & §8.1)
	adnResolver := dns.NewDoTResolver(serverAddr, "localhost").
		WithPrivacyProfile(dns.PrivacyProfileStrict).
		WithAuthenticationDomainName("dns.example.com")
	adnResolver.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	adnIPs, errADN := adnResolver.LookupIPAddr(t.Context(), "example.com")
	require.NoError(t, errADN)
	require.NotEmpty(t, adnIPs)

	// 3. Strict Privacy with mismatched ADN -> MUST fail hard (RFC 8310 §8.1)
	badADNResolver := dns.NewDoTResolver(serverAddr, "localhost").
		WithPrivacyProfile(dns.PrivacyProfileStrict).
		WithAuthenticationDomainName("rogue.attacker.com")
	badADNResolver.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	_, errBadADN := badADNResolver.LookupIPAddr(t.Context(), "example.com")
	require.Error(t, errBadADN)
	assert.ErrorIs(t, errBadADN, dns.ErrStrictPrivacyFailed)
	assert.ErrorIs(t, errBadADN, dns.ErrADNAuthenticationFailed)

	// 4. Combined Authentication: Matching ADN + Matching SPKI pin -> Success (RFC 8310 §6.4)
	comboResolver := dns.NewDoTResolver(serverAddr, "localhost").
		WithPrivacyProfile(dns.PrivacyProfileStrict).
		WithAuthenticationDomainName("dns.example.com").
		WithSPKIPins(validPin)
	comboResolver.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	comboIPs, errCombo := comboResolver.LookupIPAddr(t.Context(), "example.com")
	require.NoError(t, errCombo)
	require.NotEmpty(t, comboIPs)

	// 5. Combined Authentication: Matching ADN + Mismatched SPKI pin -> Failure (RFC 8310 §6.4)
	badComboResolver := dns.NewDoTResolver(serverAddr, "localhost").
		WithPrivacyProfile(dns.PrivacyProfileStrict).
		WithAuthenticationDomainName("dns.example.com").
		WithSPKIPins("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	badComboResolver.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	_, errBadCombo := badComboResolver.LookupIPAddr(t.Context(), "example.com")
	require.Error(t, errBadCombo)
	assert.ErrorIs(t, errBadCombo, dns.ErrStrictPrivacyFailed)
}
