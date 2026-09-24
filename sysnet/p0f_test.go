package sysnet

import (
	"net"
	"testing"
)

func TestP0fSignature(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			if tcpConn, ok := conn.(*net.TCPConn); ok {
				if raw, err := tcpConn.SyscallConn(); err == nil {
					ApplyP0fSignature(raw, 64, 65535, true, true)
				}
			}
		}
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	if tcpConn, ok := client.(*net.TCPConn); ok {
		if raw, err := tcpConn.SyscallConn(); err == nil {
			ApplyP0fSignature(raw, 64, 65535, true, true)
		}
	}
	client.Close()
	<-done
}
