// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
)

func TestFragmentedConnSmallWrite(t *testing.T) {
	server, client := net.Pipe()

	frag := &aoni.FragmentConfig{
		ChunkSize: 10,
		MaxDelay:  0,
	}

	fragConn := aoni.NewFragmentedConn(client, frag)

	data := []byte("hello")
	go func() {
		buf := make([]byte, 1024)
		n, _ := server.Read(buf)
		server.Close()

		if !bytes.Equal(buf[:n], data) {
			t.Errorf("got %q, want %q", buf[:n], data)
		}
	}()

	n, err := fragConn.Write(data)
	if err != nil {
		t.Fatal(err)
	}

	if n != len(data) {
		t.Errorf("wrote %d bytes, want %d", n, len(data))
	}

	fragConn.Close()
}

func TestFragmentedConnLargeWrite(t *testing.T) {
	server, client := net.Pipe()

	frag := &aoni.FragmentConfig{
		ChunkSize: 5,
		MaxDelay:  0,
	}

	fragConn := aoni.NewFragmentedConn(client, frag)

	data := []byte("hello world test data")

	var received []byte

	done := make(chan struct{})
	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		for {
			n, err := server.Read(buf)
			if n > 0 {
				received = append(received, buf[:n]...)
			}

			if err != nil {
				break
			}
		}
	}()

	_, err := fragConn.Write(data)
	if err != nil {
		t.Fatal(err)
	}

	fragConn.Close()
	<-done

	if !bytes.Equal(received, data) {
		t.Errorf("received %q, want %q", received, data)
	}
}

func TestWithFragmentation(t *testing.T) {
	mod := aoni.WithFragmentation(aoni.FragmentConfig{
		ChunkSize: 100,
		MaxDelay:  10 * time.Millisecond,
	})

	if mod == nil {
		t.Error("WithFragmentation returned nil")
	}
}

func TestNewFragmentedConn(t *testing.T) {
	server, client := net.Pipe()

	cfg := &aoni.FragmentConfig{
		ChunkSize: 10,
		MaxDelay:  5 * time.Millisecond,
	}

	fragConn := aoni.NewFragmentedConn(client, cfg)

	data := []byte("test data for fragmentation")

	var received []byte

	done := make(chan struct{})

	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		for {
			n, err := server.Read(buf)
			if n > 0 {
				received = append(received, buf[:n]...)
			}

			if err != nil {
				break
			}
		}
	}()

	_, err := fragConn.Write(data)
	if err != nil {
		t.Fatal(err)
	}

	fragConn.Close()
	server.Close()
	<-done

	if !bytes.Equal(received, data) {
		t.Errorf("received %q, want %q", received, data)
	}
}
