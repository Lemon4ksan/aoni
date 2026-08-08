// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package transport provides L4 (TCP/UDP) and L7 (TLS/uTLS) network
// connection utilities for the aoni project.
package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/internal/h1"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/cert"
	"github.com/lemon4ksan/aoni/netutil/fragment"
	"github.com/lemon4ksan/aoni/netutil/ip"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

var (
	ErrServerH2NotSupported = errors.New("aoni transport: server does not support HTTP/2 ALPN")
	ErrTargetURLEmpty       = errors.New("aoni transport: target address is empty")
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
	AutoECH         bool
	Enable0RTT      bool
}

// UniversalDialer is a thread-safe, stateless L4/L7 execution engine.
// It serves as the single source of truth for all socket connection dialing
// across standard HTTP, fasthttp, WebSockets, gRPC, and MASQUE tunnels.
type UniversalDialer struct {
	activeHTTPS map[string]int
	activeMu    sync.RWMutex
}

// NewUniversalDialer initializes a new UniversalDialer.
func NewUniversalDialer() *UniversalDialer {
	return &UniversalDialer{
		activeHTTPS: make(map[string]int),
	}
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

	return netdial.DialL4(ctx, network, targetAddr, dialOpts)
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

	dialOpts := buildNetdialOptions(cfg)

	// Plain HTTP fallback for explicit port 80 requests
	if port == "80" && strings.HasSuffix(addr, ":80") {
		return netdial.DialL4(ctx, network, targetAddr, dialOpts)
	}

	rawConn, err := netdial.DialL4(ctx, network, targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	trackingConn := netutil.NewWriteTrackingConn(rawConn)

	// Use uTLS fingerprinting if HelloID, SpecProvider, or custom TLS settings are active
	if cfg.HelloID != nil || cfg.SpecProvider != nil {
		return d.handshakeUTLS(ctx, trackingConn, host, cfg)
	}

	// Standard Go crypto/tls fallback
	return d.handshakeStandardTLS(ctx, rawConn, host, dialOpts, cfg)
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

func (d *UniversalDialer) handshakeUTLS(ctx context.Context, conn net.Conn, host string, cfg DialConfig) (net.Conn, error) {
	utlsOpts := netdial.RTLSOptions{
		HelloID:            cfg.HelloID,
		SpecProvider:       cfg.SpecProvider,
		SessionCache:       cfg.SessionCache,
		CertificatePins:    cfg.CertificatePins,
		CertCompression:    cfg.CertCompression,
		JA4Callback:        cfg.JA4Callback,
		BaseTLSConfig:      cfg.BaseTLSConfig,
		ALPNOverride:       cfg.ALPNOverride,
		ECHConfigList:      cfg.ECHConfigList,
		AutoECH:            cfg.AutoECH,
		InsecureSkipVerify: cfg.InsecureSkipVerify || (cfg.BaseTLSConfig != nil && cfg.BaseTLSConfig.InsecureSkipVerify),
		DNSResolver:        cfg.DNSResolver,
	}

	if utlsOpts.BaseTLSConfig == nil {
		utlsOpts.BaseTLSConfig = &tls.Config{}
	}

	serverName := resolveServerName(cfg.ServerName, host)
	if utlsOpts.BaseTLSConfig.ServerName == "" && serverName != "" {
		utlsOpts.BaseTLSConfig.ServerName = serverName
	}

	if len(utlsOpts.ALPNOverride) == 0 {
		if utlsOpts.BaseTLSConfig != nil && len(utlsOpts.BaseTLSConfig.NextProtos) > 0 {
			utlsOpts.ALPNOverride = utlsOpts.BaseTLSConfig.NextProtos
		} else {
			utlsOpts.ALPNOverride = []string{"h2", "http/1.1"}
		}
	}

	uConn, report, err := netdial.HandshakeUTLS(ctx, conn, host, utlsOpts)
	if err != nil {
		return nil, err
	}

	if cfg.JA4ReportStore != nil {
		report.JA4H = cfg.JA4ReportStore.JA4H
		*cfg.JA4ReportStore = report
	}

	negotiatedProto := uConn.ConnectionState().NegotiatedProtocol

	wrappedConn := &uTLSConnWrapper{uConn}

	// If HTTP/1.1 was negotiated and custom header ordering is requested, wrap in HeaderOrderingConn
	if negotiatedProto != "h2" && len(cfg.HeaderOrder) > 0 {
		return &h1.HeaderOrderingConn{
			Conn:        wrappedConn,
			OrderedKeys: cfg.HeaderOrder,
		}, nil
	}

	return wrappedConn, nil
}

type uTLSConnWrapper struct {
	*netdial.UConnWrapper
}

func (w *uTLSConnWrapper) Handshake() error {
	return nil
}

func (w *uTLSConnWrapper) ConnectionState() tls.ConnectionState {
	uState := w.UConn.ConnectionState()
	return tls.ConnectionState{
		Version:                    uState.Version,
		HandshakeComplete:          true,
		DidResume:                  uState.DidResume,
		CipherSuite:                uState.CipherSuite,
		NegotiatedProtocol:         uState.NegotiatedProtocol,
		NegotiatedProtocolIsMutual: true,
		ServerName:                 uState.ServerName,
		PeerCertificates:           uState.PeerCertificates,
		VerifiedChains:             uState.VerifiedChains,
	}
}

func (d *UniversalDialer) handshakeStandardTLS(
	ctx context.Context,
	conn net.Conn,
	host string,
	dialOpts netdial.DialOptions,
	cfg DialConfig,
) (net.Conn, error) {
	baseCfg := cfg.BaseTLSConfig
	if baseCfg == nil {
		baseCfg = &tls.Config{}
	}

	tlsCfg := baseCfg
	serverName := resolveServerName(cfg.ServerName, host)

	if tlsCfg.ServerName == "" && serverName != "" {
		cloned := tlsCfg.Clone()
		cloned.ServerName = serverName
		tlsCfg = cloned
	}

	if dialOpts.InsecureSkipVerify {
		cloned := tlsCfg.Clone()
		cloned.InsecureSkipVerify = true
		tlsCfg = cloned
	}

	tlsConn := tls.Client(conn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return tlsConn, nil
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

func applyDelay(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
