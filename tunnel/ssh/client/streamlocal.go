// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

type streamLocalPayload struct {
	SocketPath string
	Reserved   string
	Reserved2  uint32
}

// DialUnix establishes a streamlocal UNIX domain socket connection on the remote host via SSH.
func (c *Client) DialUnix(ctx context.Context, remoteSocketPath string) (net.Conn, error) {
	if c.closed.Load() || c.Client == nil {
		return nil, ErrSSHClosed
	}

	payload := ssh.Marshal(streamLocalPayload{
		SocketPath: remoteSocketPath,
		Reserved:   "",
		Reserved2:  0,
	})

	type result struct {
		ch  ssh.Channel
		err error
	}

	resCh := make(chan result, 1)

	go func() {
		ch, reqs, err := c.OpenChannel("direct-streamlocal@openssh.com", payload)
		if err == nil {
			go ssh.DiscardRequests(reqs)
		}

		resCh <- result{ch: ch, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return nil, fmt.Errorf("aoni ssh streamlocal: open channel failed: %w", res.err)
		}

		return &unixConnBridge{Channel: res.ch}, nil
	}
}

type unixConnBridge struct {
	ssh.Channel
}

func (c *unixConnBridge) LocalAddr() net.Addr {
	return &net.UnixAddr{Name: "local", Net: "unix"}
}

func (c *unixConnBridge) RemoteAddr() net.Addr {
	return &net.UnixAddr{Name: "remote", Net: "unix"}
}

func (c *unixConnBridge) SetDeadline(_ time.Time) error      { return nil }
func (c *unixConnBridge) SetReadDeadline(_ time.Time) error  { return nil }
func (c *unixConnBridge) SetWriteDeadline(_ time.Time) error { return nil }
