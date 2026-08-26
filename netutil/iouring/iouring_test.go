// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package iouring_test

import (
	"context"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/netutil/iouring"
)

func TestIOUring_NonLinuxFallback(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("skipping non-linux test on linux")
	}

	_, err := iouring.New(32)
	if err != iouring.ErrIOUringUnsupported {
		t.Fatalf("expected ErrIOUringUnsupported on %s, got %v", runtime.GOOS, err)
	}

	_, err = iouring.DialIOUring(context.Background(), "tcp", "127.0.0.1:80")
	if err != iouring.ErrIOUringUnsupported {
		t.Fatalf("expected ErrIOUringUnsupported on %s, got %v", runtime.GOOS, err)
	}
}

func TestIOUring_EchoServer_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping io_uring socket test on non-linux OS")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			_, _ = conn.Write(buf[:n])
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := iouring.DialIOUring(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial io_uring socket: %v", err)
	}
	defer conn.Close()

	payload := []byte("HELLO_IO_URING_SILICON_SPEED")
	n, err := conn.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("write failed: %v (wrote %d)", err, n)
	}

	readBuf := make([]byte, len(payload))
	n, err = io.ReadFull(conn, readBuf)
	if err != nil || n != len(payload) {
		t.Fatalf("read failed: %v (read %d)", err, n)
	}

	if string(readBuf) != string(payload) {
		t.Fatalf("mismatched payload: got %q, want %q", string(readBuf), string(payload))
	}
}

func BenchmarkIOUring_ReadWrite_64B(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("skipping io_uring benchmark on non-linux OS")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	conn, err := iouring.DialIOUring(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	payload := make([]byte, 64)
	readBuf := make([]byte, 64)

	b.SetBytes(64)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := conn.Write(payload)
		if err != nil {
			b.Fatal(err)
		}
		_, err = io.ReadFull(conn, readBuf)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIOUring_ReadWrite_1KB(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("skipping io_uring benchmark on non-linux OS")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	conn, err := iouring.DialIOUring(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	payload := make([]byte, 1024)
	readBuf := make([]byte, 1024)

	b.SetBytes(1024)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := conn.Write(payload)
		if err != nil {
			b.Fatal(err)
		}
		_, err = io.ReadFull(conn, readBuf)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIOUring_Parallel_RPS(b *testing.B) {
	if runtime.GOOS != "linux" {
		b.Skip("skipping io_uring benchmark on non-linux OS")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				resp := []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
				for {
					n, err := c.Read(buf)
					if err != nil || n == 0 {
						return
					}
					_, _ = c.Write(resp)
				}
			}(conn)
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		conn, err := iouring.DialIOUring(context.Background(), "tcp", ln.Addr().String())
		if err != nil {
			b.Fatal(err)
		}
		defer conn.Close()

		req := []byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
		buf := make([]byte, 128)

		for pb.Next() {
			_, err := conn.Write(req)
			if err != nil {
				b.Fatal(err)
			}
			_, err = conn.Read(buf)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
