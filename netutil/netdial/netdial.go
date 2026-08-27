// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package netdial provides utilities for network dialing and proxy configuration.
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
	"sync"
	"syscall"
	"time"

	fio "github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/ip"
	"github.com/lemon4ksan/foundation/net/proxy"

	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/netutil/fragment"
)

var (
	// ErrSSRFBlocked is returned when the request is blocked by the SSRF guard.
	ErrSSRFBlocked = errors.New("aoni/netdial: request blocked by SSRF guard")
	// ErrEmptyProxyURL is returned when the proxy target address is empty.
	ErrEmptyProxyURL = errors.New("aoni/netdial: proxy target address is empty")
	// ErrProxyConnectFailed is returned when the proxy connection fails.
	ErrProxyConnectFailed = errors.New("aoni/netdial: proxy connection failed")
)

// Network represents an L4 transport or IPC socket network protocol (e.g. "tcp", "unix").
type Network string

const (
	// NetworkTCP represents Transmission Control Protocol over IPv4 or IPv6 ("tcp").
	NetworkTCP Network = "tcp"

	// NetworkTCP4 represents Transmission Control Protocol restricted to IPv4 ("tcp4").
	NetworkTCP4 Network = "tcp4"

	// NetworkTCP6 represents Transmission Control Protocol restricted to IPv6 ("tcp6").
	NetworkTCP6 Network = "tcp6"

	// NetworkUDP represents User Datagram Protocol over IPv4 or IPv6 ("udp").
	NetworkUDP Network = "udp"

	// NetworkUDP4 represents User Datagram Protocol restricted to IPv4 ("udp4").
	NetworkUDP4 Network = "udp4"

	// NetworkUDP6 represents User Datagram Protocol restricted to IPv6 ("udp6").
	NetworkUDP6 Network = "udp6"

	// NetworkIP represents raw IP protocol over IPv4 or IPv6 ("ip").
	NetworkIP Network = "ip"

	// NetworkIP4 represents raw IP protocol restricted to IPv4 ("ip4").
	NetworkIP4 Network = "ip4"

	// NetworkIP6 represents raw IP protocol restricted to IPv6 ("ip6").
	NetworkIP6 Network = "ip6"

	// NetworkUnix represents Unix domain stream socket ("unix").
	NetworkUnix Network = "unix"

	// NetworkUnixGram represents Unix domain datagram socket ("unixgram").
	NetworkUnixGram Network = "unixgram"

	// NetworkUnixPacket represents Unix domain sequenced packet socket ("unixpacket").
	NetworkUnixPacket Network = "unixpacket"
)

// String returns the network protocol string value.
func (n Network) String() string {
	return string(n)
}

// IsTCP reports whether the network is a TCP variant ("tcp", "tcp4", "tcp6").
func (n Network) IsTCP() bool {
	return n == NetworkTCP || n == NetworkTCP4 || n == NetworkTCP6
}

// IsUDP reports whether the network is a UDP variant ("udp", "udp4", "udp6").
func (n Network) IsUDP() bool {
	return n == NetworkUDP || n == NetworkUDP4 || n == NetworkUDP6
}

// IsUnix reports whether the network is a Unix domain socket variant ("unix", "unixgram", "unixpacket").
func (n Network) IsUnix() bool {
	return n == NetworkUnix || n == NetworkUnixGram || n == NetworkUnixPacket
}

// IsIP reports whether the network is a raw IP socket variant ("ip", "ip4", "ip6").
func (n Network) IsIP() bool {
	return n == NetworkIP || n == NetworkIP4 || n == NetworkIP6
}

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
	StackDriver          RawStackDriver
	L2Device             L2Device
	SourceRotator        *ip.SourceIPRotator
	P0fSignature         *p0f.Signature
	SocketController     SocketController
	FragmentConfig       *fragment.Config
	InterfaceName        string
	SocketMark           uint32
	HappyEyeballs        time.Duration
	ProxyTimeout         time.Duration
	BusyPollMicroseconds int
	SSRFGuard            bool
	ProxyDNS             bool
	InsecureSkipVerify   bool
	TCPQuickACK          bool
	RegisteredIO         bool
}

// DialL4 establishes a low-latency L4 socket connection applying DNS resolution, SSRF guards,
// IPv6 subnet rotation, p0f TCP/IP stack spoofing, and optional TCP packet fragmentation.
//
// Target addr must be formatted as "host:port", "host", or "unix:///path/to/socket".
// Yields an active, tuned [net.Conn] socket configured with TCP_NODELAY and socket buffer tuning.
func DialL4(ctx context.Context, network, addr string, opts DialOptions) (net.Conn, error) {
	if opts.StackDriver != nil {
		return opts.StackDriver.DialL4(ctx, network, addr, opts)
	}

	if opts.L2Device != nil {
		return NewL2FrameConn(opts.L2Device, nil, nil), nil
	}

	if opts.ProxyURL != nil && opts.ProxyURL.Host != "" {
		host, port, _ := net.SplitHostPort(addr)
		return DialProxy(ctx, opts.ProxyURL, host, port, opts)
	}

	if strings.HasPrefix(addr, "unix://") || network == NetworkUnix.String() {
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

// DialDirectTCP establishes a direct TCP socket connection trying all resolved IP addresses in sequence.
// Returns an active TCP socket or an aggregated connection error if all resolved IPs fail.
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

		if opts.RegisteredIO {
			return DialRIOSocket(ctx, network, target, opts)
		}

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
		return nil, fmt.Errorf("aoni/netdial: no IP addresses found for host %s", host)
	}

	orderedAddrs := orderHappyEyeballsAddrs(addrs)
	if len(orderedAddrs) > 1 {
		conn, err := dialHappyEyeballs(ctx, network, port, orderedAddrs, opts)
		if err == nil {
			if opts.FragmentConfig != nil {
				return fragment.NewFragmentedConn(conn, opts.FragmentConfig), nil
			}

			return conn, nil
		}
	}

	ipTimeout := 3 * time.Second
	if opts.HappyEyeballs > 0 {
		ipTimeout = opts.HappyEyeballs
	}

	var lastErr error
	for _, address := range orderedAddrs {
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

	return nil, fmt.Errorf("aoni/netdial: all IP connections failed for %s: %w", host, lastErr)
}

// orderHappyEyeballsAddrs sorts IP addresses per RFC 8305 §4: interleaving IPv6 and IPv4 families.
func orderHappyEyeballsAddrs(addrs []net.IPAddr) []net.IPAddr {
	if len(addrs) <= 1 {
		return addrs
	}

	var v6, v4 []net.IPAddr
	for _, a := range addrs {
		if a.IP.To4() != nil {
			v4 = append(v4, a)
		} else {
			v6 = append(v6, a)
		}
	}

	if len(v6) == 0 {
		return v4
	}

	if len(v4) == 0 {
		return v6
	}

	interleaved := make([]net.IPAddr, 0, len(addrs))

	maxLen := max(len(v6), len(v4))
	for i := range maxLen {
		if i < len(v6) {
			interleaved = append(interleaved, v6[i])
		}

		if i < len(v4) {
			interleaved = append(interleaved, v4[i])
		}
	}

	return interleaved
}

// dialHappyEyeballs performs staggered RFC 8305 connection racing across candidate IP addresses.
func dialHappyEyeballs(
	ctx context.Context,
	network, port string,
	addrs []net.IPAddr,
	opts DialOptions,
) (net.Conn, error) {
	staggerDelay := opts.HappyEyeballs
	if staggerDelay <= 0 {
		staggerDelay = 300 * time.Millisecond
	}

	type dialResult struct {
		conn net.Conn
		err  error
	}

	raceCtx, raceCancel := context.WithCancel(ctx)
	defer raceCancel()

	results := make(chan dialResult, len(addrs))

	var wg sync.WaitGroup

	for i, address := range addrs {
		if opts.SSRFGuard && ip.IsPrivateIP(address.IP) {
			continue
		}

		if i > 0 {
			timer := time.NewTimer(staggerDelay)
			select {
			case <-raceCtx.Done():
				timer.Stop()
				return nil, raceCtx.Err()
			case <-timer.C:
			case res := <-results:
				timer.Stop()

				if res.conn != nil {
					return res.conn, nil
				}
			}
		}

		target := net.JoinHostPort(address.IP.String(), port)

		wg.Add(1)

		go func(targetAddr string) {
			defer wg.Done()

			dialer := &net.Dialer{
				Timeout: 10 * time.Second,
			}
			if opts.SourceRotator != nil {
				dialer.LocalAddr = &net.TCPAddr{IP: opts.SourceRotator.Next()}
			}

			dialer.Control = buildSocketControl(opts)

			conn, err := dialer.DialContext(raceCtx, network, targetAddr)
			if err != nil {
				results <- dialResult{err: err}
				return
			}

			if raceCtx.Err() != nil {
				_ = conn.Close()
				return
			}

			results <- dialResult{conn: conn}
		}(target)
	}

	var lastErr error
	for range addrs {
		select {
		case res := <-results:
			if res.conn != nil {
				raceCancel()

				go func() {
					wg.Wait()
					close(results)

					for r := range results {
						if r.conn != nil {
							_ = r.conn.Close()
						}
					}
				}()

				return res.conn, nil
			}

			lastErr = res.err

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
}

// DialProxy establishes a network socket connection through a SOCKS5 or HTTP CONNECT proxy.
func DialProxy(ctx context.Context, proxyURL *url.URL, host, port string, opts DialOptions) (net.Conn, error) {
	if proxyURL == nil || proxyURL.Host == "" {
		return nil, ErrEmptyProxyURL
	}

	timeout := opts.ProxyTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	dialer := &net.Dialer{
		Timeout:       timeout,
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

	return dialer.DialContext(ctx, NetworkUnix.String(), socketPath)
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

	socksDialer, err := proxy.SOCKS5(NetworkTCP.String(), proxyURL.Host, auth, forward)
	if err != nil {
		return nil, fmt.Errorf("%w: create socks5 dialer: %w", ErrProxyConnectFailed, err)
	}

	if cd, ok := socksDialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, NetworkTCP.String(), net.JoinHostPort(host, port))
	}

	return socksDialer.Dial(NetworkTCP.String(), net.JoinHostPort(host, port))
}

func dialHTTPProxy(ctx context.Context, proxyURL *url.URL, forward *net.Dialer, host, port string) (net.Conn, error) {
	conn, err := forward.DialContext(ctx, NetworkTCP.String(), proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("%w: dial proxy %s: %w", ErrProxyConnectFailed, proxyURL.Host, err)
	}

	target := net.JoinHostPort(host, port)
	// RFC 9112 §3.2.3: The authority-form (host:port) is used exclusively for HTTP CONNECT requests.
	// RFC 9112 §3.2: HTTP/1.1 client MUST send a Host header matching the target authority.
	connectReqStr := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"

	if _, err := conn.Write([]byte(connectReqStr)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: send CONNECT: %w", ErrProxyConnectFailed, err)
	}

	br := bufio.NewReader(conn)

	// RFC 9112 §6.3 Rule 2: 2xx responses to CONNECT imply the connection becomes a raw tunnel
	// immediately following the header section; message body and Transfer-Encoding are ignored.
	// RFC 9931 §8: Proxy clients MUST wait for a 2xx response before forwarding TCP payload data
	// to prevent Request Smuggling via optimistic protocol transitions.
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

	// RFC 9931 §8: On CONNECT rejection, the connection MUST be closed immediately to prevent
	// subsequent payload bytes from being misinterpreted as pipelined HTTP/1.1 requests.
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: CONNECT rejected with status %s", ErrProxyConnectFailed, resp.Status)
	}

	_ = conn.SetDeadline(time.Time{})

	if br.Buffered() > 0 {
		return &fio.BufferedConn{Conn: conn, R: br}, nil
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
