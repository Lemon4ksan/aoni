// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"context"
	"net"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/h1"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

type fastDialer struct {
	config *aoni.Config
}

func newFastDialer(cfg *aoni.Config) *fastDialer {
	return &fastDialer{config: cfg}
}

// Dial executes an L4 TCP dial followed by an optional uTLS handshake,
// wrapping HTTP/1.1 connections with header ordering decorators when configured.
func (d *fastDialer) Dial(addr string) (net.Conn, error) {
	ctx := context.Background()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "80"
	}

	host = netutil.CleanHost(host)
	targetAddr := net.JoinHostPort(host, port)

	isTLS := port == "443" || d.isTLSEnabled()

	dialOpts := netdial.DialOptions{
		ProxyURL:           d.config.Network.ProxyAddr,
		DNSResolver:        d.config.Network.DNSResolver,
		SourceRotator:      d.config.Network.SourceRotator,
		P0fSignature:       d.config.Fingerprint.P0fSignature,
		SocketController:   d.config.Network.SocketController,
		FragmentConfig:     d.config.Network.FragmentConfig,
		HappyEyeballs:      d.config.Network.HappyEyeballsDelay,
		SSRFGuard:          d.config.Network.SSRFGuard,
		ProxyDNS:           d.config.Network.ProxyDNS,
		InsecureSkipVerify: d.config.Engine.InsecureSkipVerify,
	}

	rawConn, err := netdial.DialL4(ctx, "tcp", targetAddr, dialOpts)
	if err != nil {
		return nil, err
	}

	// Track written bytes for zero-byte write idle retry logic
	trackingConn := netutil.NewWriteTrackingConn(rawConn)

	var (
		conn            net.Conn = trackingConn
		negotiatedProto string
	)

	if isTLS {
		utlsOpts := netdial.RTLSOptions{
			HelloID:            d.resolveHelloID(),
			SpecProvider:       d.config.Fingerprint.TLSClientHelloSpecProvider,
			SessionCache:       d.config.Fingerprint.SessionCache,
			CertificatePins:    d.config.Fingerprint.CertificatePins,
			JA4Callback:        d.config.Fingerprint.JA4Callback,
			InsecureSkipVerify: d.config.Engine.InsecureSkipVerify,
		}

		uConn, _, err := netdial.HandshakeUTLS(ctx, trackingConn, host, utlsOpts)
		if err != nil {
			return nil, err
		}

		conn = uConn
		negotiatedProto = uConn.ConnectionState().NegotiatedProtocol
	}

	if negotiatedProto != aoni.AlpnH2 && len(d.config.Fingerprint.HeaderOrder) > 0 {
		return &h1.HeaderOrderingConn{
			Conn:        conn,
			OrderedKeys: d.config.Fingerprint.HeaderOrder,
		}, nil
	}

	return conn, nil
}

// DialH2 dials an L4 connection and performs a uTLS handshake forcing ALPN "h2".
func (d *fastDialer) DialH2(ctx context.Context, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	dialOpts := netdial.DialOptions{
		ProxyURL:           d.config.Network.ProxyAddr,
		DNSResolver:        d.config.Network.DNSResolver,
		SourceRotator:      d.config.Network.SourceRotator,
		P0fSignature:       d.config.Fingerprint.P0fSignature,
		SocketController:   d.config.Network.SocketController,
		FragmentConfig:     d.config.Network.FragmentConfig,
		HappyEyeballs:      d.config.Network.HappyEyeballsDelay,
		SSRFGuard:          d.config.Network.SSRFGuard,
		ProxyDNS:           d.config.Network.ProxyDNS,
		InsecureSkipVerify: d.config.Engine.InsecureSkipVerify,
	}

	rawConn, err := netdial.DialL4(ctx, "tcp", addr, dialOpts)
	if err != nil {
		return nil, err
	}

	utlsOpts := netdial.RTLSOptions{
		HelloID:            d.resolveHelloID(),
		SpecProvider:       d.config.Fingerprint.TLSClientHelloSpecProvider,
		SessionCache:       d.config.Fingerprint.SessionCache,
		CertificatePins:    d.config.Fingerprint.CertificatePins,
		ALPNOverride:       []string{aoni.AlpnH2},
		JA4Callback:        d.config.Fingerprint.JA4Callback,
		InsecureSkipVerify: d.config.Engine.InsecureSkipVerify,
	}

	uConn, _, err := netdial.HandshakeUTLS(ctx, rawConn, host, utlsOpts)

	return uConn, err
}

func (d *fastDialer) isTLSEnabled() bool {
	f := d.config.Fingerprint
	return f.BrowserID != aoni.BrowserNone ||
		f.TLSClientHelloID != nil ||
		f.TLSClientHelloSpecProvider != nil
}

func (d *fastDialer) resolveHelloID() *utls.ClientHelloID {
	f := d.config.Fingerprint

	if f.TLSClientHelloID != nil {
		return f.TLSClientHelloID
	}

	switch f.BrowserID {
	case aoni.BrowserFirefox:
		return &utls.HelloFirefox_Auto
	case aoni.BrowserSafari:
		return &utls.HelloSafari_Auto
	default:
		return &utls.HelloChrome_Auto
	}
}
