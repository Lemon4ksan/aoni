// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lemon4ksan/aoni/x/webtransport"
)

func generateSelfSignedCert() (tls.Certificate, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"aoni WebTransport Standalone Server"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour * 10), // Chrome limit: 14 days
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, nil, err
	}

	hash := sha256.Sum256(certDER)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}

	return tlsCert, hash[:], nil
}

func main() {
	_, certHash, err := generateSelfSignedCert()
	if err != nil {
		log.Fatalf("Failed to generate test certificate: %v", err)
	}

	fmt.Println("================================================================================")
	fmt.Println("🚀 aoni WebTransport over HTTP/3 Standalone Echo Server (draft-16 / RFC 9220)")
	fmt.Println("================================================================================")
	fmt.Printf("Server Certificate SHA-256 Hash:\n")
	fmt.Printf("  Buffer: %v\n\n", certHash)

	fmt.Println("To test this directly in Google Chrome (DevTools Console):")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf(`
const certHash = new Uint8Array(%v);
const transport = new WebTransport("https://127.0.0.1:4433/wt", {
    serverCertificateHashes: [{ algorithm: "sha-256", value: certHash }]
});

await transport.ready;
console.log("✅ WebTransport Session Ready!");

// Test Datagrams
const writer = transport.datagrams.writable.getWriter();
await writer.write(new Uint8Array([1, 2, 3, 4, 5]));
console.log("Datagram sent!");

const reader = transport.datagrams.readable.getReader();
const { value } = await reader.read();
console.log("Echoed Datagram received from aoni server:", value);

// Test Bidirectional Stream
const bidi = await transport.createBidirectionalStream();
const bidiWriter = bidi.writable.getWriter();
await bidiWriter.write(new TextEncoder().encode("Hello from Chrome WebTransport!"));
bidiWriter.close();

const bidiReader = bidi.readable.getReader();
const streamData = await bidiReader.read();
console.log("Echoed Stream Data from aoni server:", new TextDecoder().decode(streamData.value));
`, formatJSArray(certHash))
	fmt.Println("--------------------------------------------------------------------------------")

	server := webtransport.NewServer(webtransport.ServerConfig{
		Handler: webtransport.SessionHandlerFunc(func(sess *webtransport.Session) {
			log.Printf("[WebTransport] New session established: ID=%d", sess.SessionID())
			ctx := context.Background()

			// Echo Bidirectional streams
			go func() {
				for {
					str, sErr := sess.AcceptStream(ctx)
					if sErr != nil {
						return
					}
					go func(s *webtransport.Stream) {
						defer s.Close()
						log.Printf("[WebTransport] Echoing bidirectional stream...")
						_, _ = io.Copy(s, s)
					}(str)
				}
			}()

			// Echo Datagrams
			go func() {
				for {
					dgram, dErr := sess.ReceiveDatagram(ctx)
					if dErr != nil {
						return
					}
					log.Printf("[WebTransport] Echoing datagram (%d bytes)...", len(dgram))
					_ = sess.SendDatagram(dgram)
				}
			}()
		}),
	})
	defer server.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	fmt.Println("\nPress Ctrl+C to terminate the server.")
	<-sigCh
	fmt.Println("Shutting down WebTransport server...")
}

func formatJSArray(b []byte) string {
	res := "["
	for i, v := range b {
		if i > 0 {
			res += ", "
		}
		res += fmt.Sprintf("%d", v)
	}
	res += "]"
	return res
}
