// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/proxy"

	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ip"
)

var (
	// ErrSSRFBlocked is returned when the request is blocked by the SSRF guard.
	ErrSSRFBlocked = errors.New("aoni netdial: request blocked by SSRF guard")
	// ErrEmptyProxyURL is returned when the proxy target address is empty.
	ErrEmptyProxyURL = errors.New("aoni netdial: proxy target address is empty")
	// ErrProxyConnectFailed is returned when the proxy connection fails.
	ErrProxyConnectFailed = errors.New("aoni netdial: proxy connection failed")
)

// SocketController is an interface for controlling socket operations.
type SocketController interface {
	Control(fd uintptr, network, address string) error
}

// DNSResolver is an interface for resolving DNS hostnames to IP addresses.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// DialOptions aggregates lower-level L4 socket, busy poll, and proxy configuration parameters.
type DialOptions struct {
	ProxyURL             *url.URL
	DNSResolver          DNSResolver
	SourceRotator        *ip.SourceIPRotator
	P0fSignature         *p0f.Signature
	SocketController     SocketController
	FragmentConfig       *fragment.Config
	InterfaceName        string
	SocketMark           uint32
	HappyEyeballs        time.Duration
	BusyPollMicroseconds int
	SSRFGuard            bool
	ProxyDNS             bool
	InsecureSkipVerify   bool
	TCPQuickACK          bool
}

// DialL4 establishes a raw TCP socket connection applying DNS resolution, SSRF guards, IP rotation, p0f spoofing, and fragmentation.
func DialL4(ctx context.Context, network, addr string, opts DialOptions) (net.Conn, error) {
	if opts.ProxyURL != nil && opts.ProxyURL.Host != "" {
		host, port, _ := net.SplitHostPort(addr)
		return DialProxy(ctx, opts.ProxyURL, host, port, opts)
	}

	if strings.HasPrefix(addr, "unix://") || network == "unix" {
		return dialUnixSocket(ctx, addr, opts)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "80"
	}

	if opts.ProxyDNS && opts.ProxyURL != nil && net.ParseIP(host) == nil {
		return DialProxy(ctx, opts.ProxyURL, host, port, opts)
	}

	return DialDirectTCP(ctx, network, host, port, opts)
}

// DialDirectTCP establishes a direct TCP socket connection trying all resolved IP addresses.
func DialDirectTCP(ctx context.Context, network, host, port string, opts DialOptions) (net.Conn, error) {
	resolver := opts.DNSResolver
	if resolver == nil {
		resolver = &net.Resolver{}
	}

	if ipAddr := net.ParseIP(host); ipAddr != nil {
		if opts.SSRFGuard && ip.IsPrivateIP(ipAddr) {
			return nil, fmt.Errorf("%w: %s", ErrSSRFBlocked, ipAddr.String())
		}

		target := net.JoinHostPort(host, port)

		dialer := &net.Dialer{
			Timeout: 10 * time.Second,
		}
		if opts.SourceRotator != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: opts.SourceRotator.Next()}
		}

		dialer.Control = buildSocketControl(opts)

		conn, err := dialer.DialContext(ctx, network, target)
		if err != nil {
			return nil, err
		}

		if opts.FragmentConfig != nil {
			return fragment.NewFragmentedConn(conn, opts.FragmentConfig), nil
		}

		return conn, nil
	}

	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("aoni netdial: no IP addresses found for host %s", host)
	}

	ipTimeout := 3 * time.Second
	if opts.HappyEyeballs > 0 {
		ipTimeout = opts.HappyEyeballs
	}

	var lastErr error
	for _, address := range addrs {
		if opts.SSRFGuard && ip.IsPrivateIP(address.IP) {
			lastErr = fmt.Errorf("%w: %s", ErrSSRFBlocked, address.IP.String())
			continue
		}

		target := net.JoinHostPort(address.IP.String(), port)

		ipCtx, ipCancel := context.WithTimeout(ctx, ipTimeout)

		dialer := &net.Dialer{
			Timeout: ipTimeout,
		}
		if opts.SourceRotator != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: opts.SourceRotator.Next()}
		}

		dialer.Control = buildSocketControl(opts)

		conn, err := dialer.DialContext(ipCtx, network, target)

		ipCancel()

		if err == nil {
			if opts.FragmentConfig != nil {
				return fragment.NewFragmentedConn(conn, opts.FragmentConfig), nil
			}

			return conn, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("aoni netdial: all IP connections failed for %s: %w", host, lastErr)
}

// DialProxy establishes a network socket connection through a SOCKS5 or HTTP CONNECT proxy.
func DialProxy(ctx context.Context, proxyURL *url.URL, host, port string, opts DialOptions) (net.Conn, error) {
	if proxyURL == nil || proxyURL.Host == "" {
		return nil, ErrEmptyProxyURL
	}

	dialer := &net.Dialer{
		Timeout:       30 * time.Second,
		FallbackDelay: opts.HappyEyeballs,
	}

	if opts.SourceRotator != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: opts.SourceRotator.Next()}
	}

	dialer.Control = buildSocketControl(opts)

	switch proxyURL.Scheme {
	case "socks5", "socks5h":
		return dialSocks5(ctx, proxyURL, dialer, host, port)
	default:
		return dialHTTPProxy(ctx, proxyURL, dialer, host, port)
	}
}

func dialUnixSocket(ctx context.Context, addr string, opts DialOptions) (net.Conn, error) {
	socketPath := strings.TrimPrefix(addr, "unix://")
	dialer := &net.Dialer{Timeout: 30 * time.Second}

	if opts.SocketController != nil || opts.P0fSignature != nil {
		dialer.Control = buildSocketControl(opts)
	}

	return dialer.DialContext(ctx, "unix", socketPath)
}

func dialSocks5(ctx context.Context, proxyURL *url.URL, forward proxy.Dialer, host, port string) (net.Conn, error) {
	var auth *proxy.Auth
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		auth = &proxy.Auth{
			User:     proxyURL.User.Username(),
			Password: password,
		}
	}

	socksDialer, err := proxy.SOCKS5("tcp", proxyURL.Host, auth, forward)
	if err != nil {
		return nil, fmt.Errorf("%w: create socks5 dialer: %w", ErrProxyConnectFailed, err)
	}

	if cd, ok := socksDialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	}

	return socksDialer.Dial("tcp", net.JoinHostPort(host, port))
}

func dialHTTPProxy(ctx context.Context, proxyURL *url.URL, forward *net.Dialer, host, port string) (net.Conn, error) {
	conn, err := forward.DialContext(ctx, "tcp", proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("%w: dial proxy %s: %w", ErrProxyConnectFailed, proxyURL.Host, err)
	}

	target := net.JoinHostPort(host, port)
	connectReqStr := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"

	if _, err := conn.Write([]byte(connectReqStr)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: send CONNECT: %w", ErrProxyConnectFailed, err)
	}

	br := bufio.NewReader(conn)

	// Pass a Request with Method: CONNECT so http.ReadResponse
	// recognizes that 2xx CONNECT responses contain no body.
	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: target},
	}

	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: read CONNECT response: %w", ErrProxyConnectFailed, err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: CONNECT rejected with status %s", ErrProxyConnectFailed, resp.Status)
	}

	_ = conn.SetDeadline(time.Time{})

	if br.Buffered() > 0 {
		return &io.BufferedConn{Conn: conn, R: br}, nil
	}

	return conn, nil
}

func buildSocketControl(opts DialOptions) func(network, address string, rc syscall.RawConn) error {
	var spoofer *p0f.Spoofer
	if opts.P0fSignature != nil {
		spoofer = p0f.NewSpoofer(opts.P0fSignature)
	}

	controller := opts.SocketController

	return func(network, address string, rc syscall.RawConn) error {
		var controlErr error

		err := rc.Control(func(fd uintptr) {
			if applyErr := applyLinuxSocketOptions(fd, opts); applyErr != nil {
				controlErr = applyErr
				return
			}

			if controller != nil {
				controlErr = controller.Control(fd, network, address)
			}
		})
		if err != nil {
			return err
		}

		if controlErr != nil {
			return controlErr
		}

		if spoofer != nil {
			return spoofer.ApplyToRawConn(rc)
		}

		return nil
	}
}
