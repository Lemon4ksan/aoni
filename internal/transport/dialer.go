// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/net/ip"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

var (
	ErrServerH2NotSupported = errors.New("aoni/transport: server does not support HTTP/2 ALPN")
	ErrTargetURLEmpty       = errors.New("aoni/transport: target address is empty")
)

// DialConfig is an independent, self-contained configuration DTO
// required to establish L4 (TCP/UDP) and L7 (TLS/uTLS) network connections.
// It contains ZERO references to high-level client or request structures.
type DialConfig struct {
	// L4 / Network Options
	ProxyURL             *url.URL
	DNSResolver          netdial.DNSResolver
	StackDriver          netdial.RawStackDriver
	L2Device             netdial.L2Device
	SourceRotator        *ip.SourceIPRotator
	P0fSignature         *p0f.Signature
	SocketController     netdial.SocketController
	FragmentConfig       *fragment.Config
	HostRewriteRules     map[string]string
	InterfaceName        string
	SocketMark           uint32
	HappyEyeballs        time.Duration
	TCPDelay             time.Duration
	SSRFGuard            bool
	ProxyDNS             bool
	InsecureSkipVerify   bool
	TCPQuickACK          bool
	RegisteredIO         bool
	BusyPollMicroseconds int

	// TLS / uTLS Options
	HelloID         *utls.ClientHelloID
	SpecProvider    netdial.ClientHelloSpecProvider
	SessionCache    utls.ClientSessionCache
	CertificatePins map[string][]string
	CertCompression []cert.CompressionAlgorithm
	ALPNOverride    []string
	ECHConfigList   []byte
	BaseTLSConfig   *tls.Config
	ServerName      string
	HeaderOrder     []string
	JA4Callback     func(ja4.Report)
	JA4ReportStore  *ja4.Report
	ConnFilters     []ConnFilter
	AutoECH         bool
	Enable0RTT      bool
}

// UniversalDialer is a thread-safe, stateless L4/L7 execution engine.
// It serves as the single source of truth for all socket connection dialing
// across standard HTTP, fasthttp, WebSockets, gRPC, and MASQUE tunnels.
type UniversalDialer struct {
	activeHTTPS map[string]int
}

// NewUniversalDialer initializes a new UniversalDialer.
func NewUniversalDialer() *UniversalDialer {
	return &UniversalDialer{
		activeHTTPS: make(map[string]int),
	}
}

// buildPipeline constructs the slice of active ConnFilter codecs based on DialConfig.
// If no L7 filters are needed, it returns an empty slice for zero-allocation execution.
func (d *UniversalDialer) buildPipeline(cfg *DialConfig, isTLS bool) []ConnFilter {
	if !isTLS && len(cfg.ConnFilters) == 0 {
		return nil
	}

	filters := make([]ConnFilter, 0, 1+len(cfg.ConnFilters))

	if isTLS {
		filters = append(filters, TLSHandshakeFilter)
	}

	if len(cfg.ConnFilters) > 0 {
		filters = append(filters, cfg.ConnFilters...)
	}

	return filters
}

// DialContext establishes a raw L4 TCP connection applying DNS resolution,
// SSRF guards, IP rotation, p0f spoofing, and TCP write fragmentation.
func (d *UniversalDialer) DialContext(ctx context.Context, network, addr string, cfg DialConfig) (net.Conn, error) {
	if cfg.TCPDelay > 0 {
		if err := applyDelay(ctx, cfg.TCPDelay); err != nil {
			return nil, err
		}
	}

	host, port := splitHostPortDefault(addr)
	host, port = applyRewriteRules(host, port, cfg.HostRewriteRules)
	targetAddr := net.JoinHostPort(host, port)

	dialOpts := buildNetdialOptions(cfg)

	rawConn, err := netdial.DialL4(ctx, network, targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	filters := d.buildPipeline(&cfg, false)

	return ExecutePipeline(ctx, rawConn, host, &cfg, filters)
}

// DialTLSContext establishes an encrypted TLS or uTLS connection over L4 TCP,
// negotiating ALPN tokens and applying browser ClientHello fingerprints.
func (d *UniversalDialer) DialTLSContext(ctx context.Context, network, addr string, cfg DialConfig) (net.Conn, error) {
	if cfg.TCPDelay > 0 {
		if err := applyDelay(ctx, cfg.TCPDelay); err != nil {
			return nil, err
		}
	}

	host, port := splitHostPortTLS(addr)
	host, port = applyRewriteRules(host, port, cfg.HostRewriteRules)

	targetAddr := net.JoinHostPort(host, port)
	cfg.ServerName = resolveServerName(cfg.ServerName, host)

	dialOpts := buildNetdialOptions(cfg)

	// Plain HTTP fallback for explicit port 80 requests
	if port == "80" && strings.HasSuffix(addr, ":80") {
		return netdial.DialL4(ctx, network, targetAddr, dialOpts)
	}

	rawConn, err := netdial.DialL4(ctx, network, targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	filters := d.buildPipeline(&cfg, true)

	return ExecutePipeline(ctx, rawConn, host, &cfg, filters)
}

// DialH2 dials an L4 connection and forces uTLS handshake with ALPN "h2".
func (d *UniversalDialer) DialH2(ctx context.Context, addr string, cfg DialConfig) (net.Conn, error) {
	cfg.ALPNOverride = []string{"h2", "http/1.1"}

	conn, err := d.DialTLSContext(ctx, "tcp", addr, cfg)
	if err != nil {
		return nil, err
	}

	if cs, ok := conn.(interface{ ConnectionState() tls.ConnectionState }); ok {
		if cs.ConnectionState().NegotiatedProtocol != "h2" {
			_ = conn.Close()
			return nil, ErrServerH2NotSupported
		}
	}

	return conn, nil
}

func buildNetdialOptions(cfg DialConfig) netdial.DialOptions {
	return netdial.DialOptions{
		ProxyURL:             cfg.ProxyURL,
		DNSResolver:          cfg.DNSResolver,
		InterfaceName:        cfg.InterfaceName,
		SocketMark:           cfg.SocketMark,
		StackDriver:          cfg.StackDriver,
		L2Device:             cfg.L2Device,
		SourceRotator:        cfg.SourceRotator,
		P0fSignature:         cfg.P0fSignature,
		SocketController:     cfg.SocketController,
		FragmentConfig:       cfg.FragmentConfig,
		HappyEyeballs:        cfg.HappyEyeballs,
		SSRFGuard:            cfg.SSRFGuard,
		ProxyDNS:             cfg.ProxyDNS,
		InsecureSkipVerify:   cfg.InsecureSkipVerify,
		BusyPollMicroseconds: cfg.BusyPollMicroseconds,
		TCPQuickACK:          cfg.TCPQuickACK,
		RegisteredIO:         cfg.RegisteredIO,
	}
}

func applyDelay(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func resolveServerName(customName, host string) string {
	if customName != "" {
		return customName
	}

	if net.ParseIP(host) != nil {
		return ""
	}

	if addr, err := netip.ParseAddr(host); err == nil && addr.IsValid() {
		return ""
	}

	return host
}

func splitHostPortDefault(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return netutil.CleanHost(addr), "80"
	}

	return netutil.CleanHost(h), p
}

func splitHostPortTLS(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return netutil.CleanHost(addr), "443"
	}

	return netutil.CleanHost(h), p
}

func applyRewriteRules(host, port string, rules map[string]string) (string, string) {
	if len(rules) == 0 {
		return host, port
	}

	if rewritten, exists := rules[host]; exists {
		if newHost, newPort, err := net.SplitHostPort(rewritten); err == nil {
			host = newHost

			if newPort != "" {
				port = newPort
			}
		} else if rewritten != "" {
			host = rewritten
		}
	}

	return host, port
}
