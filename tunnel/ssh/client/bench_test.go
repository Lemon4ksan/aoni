// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"path/filepath"
	"testing"
	"time"

	golangssh "golang.org/x/crypto/ssh"

	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

func BenchmarkParseKey(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	pemBlock, err := golangssh.MarshalPrivateKey(priv, "bench-key")
	if err != nil {
		b.Fatal(err)
	}

	unencryptedBytes := pem.EncodeToMemory(pemBlock)

	encBlock, err := golangssh.MarshalPrivateKeyWithPassphrase(priv, "bench-enc", []byte("secretpass"))
	if err != nil {
		b.Fatal(err)
	}

	encryptedBytes := pem.EncodeToMemory(encBlock)

	b.Run("Unencrypted", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = client.ParseKey(unencryptedBytes, "")
		}
	})

	b.Run("EncryptedPassphrase", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = client.ParseKey(encryptedBytes, "secretpass")
		}
	})
}

func BenchmarkKnownHosts(b *testing.B) {
	tempDir := b.TempDir()
	knownFile := filepath.Join(tempDir, "known_hosts")

	_, err := client.EnsureKnownHosts(knownFile)
	if err != nil {
		b.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	signer, err := golangssh.NewSignerFromKey(priv)
	if err != nil {
		b.Fatal(err)
	}

	pubKey := signer.PublicKey()
	_ = pub

	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
	host := "127.0.0.1:2222"

	if err := client.AddKnownHost(host, addr, pubKey, knownFile); err != nil {
		b.Fatal(err)
	}

	b.Run("CheckKnownHost", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = client.CheckKnownHost(host, addr, pubKey, knownFile)
		}
	})
}

func BenchmarkClientOperations(b *testing.B) {
	t := &testing.T{}
	srv := startMockServer(t)
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

	b.Run("Command Execution", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			_, _ = c.Run(t.Context(), "echo hello")
		}
	})

	b.Run("Stream Processing", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			outCh, errCh, doneCh, cmdErrCh, err := c.Stream(ctx, "stream_test", 5*time.Second)
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
}
