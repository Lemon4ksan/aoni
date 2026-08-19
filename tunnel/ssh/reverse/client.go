// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reverse provides a reverse SSH tunnel client for exposing local services.
package reverse

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ClientConfig configures an embedded reverse SSH tunnel client.
type ClientConfig struct {
	ServerAddr string
	LocalAddr  string
	Subdomain  string
	SSHConfig  *ssh.ClientConfig
}

// ExposeLocal establishes a reverse SSH tunnel forwarding public requests from server
// directly to localAddr (e.g. "127.0.0.1:3000") without requiring external shell binaries.
//
// Resilience:
// Retries connections automatically with exponential backoff if the network connection drops.
func ExposeLocal(ctx context.Context, cfg ClientConfig) error {
	if cfg.LocalAddr == "" {
		cfg.LocalAddr = "127.0.0.1:8080"
	}

	if cfg.SSHConfig == nil {
		cfg.SSHConfig = &ssh.ClientConfig{
			User:            "aoni",
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
			Timeout:         10 * time.Second,
		}
	}

	return runClientLoop(ctx, cfg)
}

func runClientLoop(ctx context.Context, cfg ClientConfig) error {
	backoff := 1 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := runSingleClientSession(ctx, cfg)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}

			backoff = min(backoff*2, 30*time.Second)

			continue
		}

		backoff = 1 * time.Second
	}
}

func runSingleClientSession(ctx context.Context, cfg ClientConfig) error {
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	conn, err := dialer.DialContext(ctx, "tcp", cfg.ServerAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, cfg.ServerAddr, cfg.SSHConfig)
	if err != nil {
		return err
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	payload := ssh.Marshal(tcpipForwardMsg{Addr: cfg.Subdomain, Rport: 80})

	ok, _, err := client.SendRequest("tcpip-forward", true, payload)
	if err != nil || !ok {
		return fmt.Errorf("aoni reverse client: tcpip-forward rejected: %w", err)
	}

	forwardedChans := client.HandleChannelOpen("forwarded-tcpip")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case newChan, open := <-forwardedChans:
			if !open {
				return nil
			}

			go handleForwardedChannel(ctx, newChan, cfg.LocalAddr)
		}
	}
}

func handleForwardedChannel(ctx context.Context, newChan ssh.NewChannel, localAddr string) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		return
	}
	defer ch.Close()

	go ssh.DiscardRequests(reqs)

	dialer := &net.Dialer{Timeout: 5 * time.Second}

	localConn, err := dialer.DialContext(ctx, "tcp", localAddr)
	if err != nil {
		return
	}
	defer localConn.Close()

	bufPtr1 := ioBufferPool.Get().(*[]byte)
	bufPtr2 := ioBufferPool.Get().(*[]byte)

	defer ioBufferPool.Put(bufPtr1)
	defer ioBufferPool.Put(bufPtr2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() { defer wg.Done(); proxyPipeWithBuf(localConn, ch, *bufPtr1) }()
	go func() { defer wg.Done(); proxyPipeWithBuf(ch, localConn, *bufPtr2) }()

	wg.Wait()
}

func proxyPipeWithBuf(dst io.Writer, src io.Reader, buf []byte) {
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if ew != nil || nw < nr {
				break
			}
		}

		if er != nil {
			break
		}
	}
}
