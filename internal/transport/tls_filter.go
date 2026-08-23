// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"

	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

// uTLSConnWrapper implements connectionState for tls.ConnectionState compatibility
type uTLSConnWrapper struct {
	*netdial.UConnWrapper
}

func (w *uTLSConnWrapper) Handshake() error {
	return nil
}

func (w *uTLSConnWrapper) ConnectionState() tls.ConnectionState {
	uState := w.UConn.ConnectionState()

	return tls.ConnectionState{
		Version:            uState.Version,
		HandshakeComplete:  true,
		DidResume:          uState.DidResume,
		CipherSuite:        uState.CipherSuite,
		NegotiatedProtocol: uState.NegotiatedProtocol,
		ServerName:         uState.ServerName,
		PeerCertificates:   uState.PeerCertificates,
		VerifiedChains:     uState.VerifiedChains,
	}
}

// TLSHandshakeFilter is the codec filter for L7 TLS/uTLS encryption, ALPN negotiation, and ECH.
func TLSHandshakeFilter(ctx context.Context, conn net.Conn, targetHost string, cfg *DialConfig) (net.Conn, error) {
	if cfg == nil {
		return conn, nil
	}

	trackingConn := netutil.NewWriteTrackingConn(conn)

	if cfg.HelloID != nil || cfg.SpecProvider != nil {
		return handshakeUTLS(ctx, trackingConn, targetHost, cfg)
	}

	return handshakeStandardTLS(ctx, conn, targetHost, cfg)
}

func handshakeUTLS(ctx context.Context, conn net.Conn, host string, cfg *DialConfig) (net.Conn, error) {
	utlsOpts := netdial.RTLSOptions{
		HelloID:         cfg.HelloID,
		SpecProvider:    cfg.SpecProvider,
		SessionCache:    cfg.SessionCache,
		CertificatePins: cfg.CertificatePins,
		CertCompression: cfg.CertCompression,
		JA4Callback:     cfg.JA4Callback,
		BaseTLSConfig:   cfg.BaseTLSConfig,
		ALPNOverride:    cfg.ALPNOverride,
		ECHConfigList:   cfg.ECHConfigList,
		AutoECH:         cfg.AutoECH,
		InsecureSkipVerify: cfg.InsecureSkipVerify ||
			(cfg.BaseTLSConfig != nil && cfg.BaseTLSConfig.InsecureSkipVerify),
		DNSResolver: cfg.DNSResolver,
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

	if negotiatedProto != "h2" && len(cfg.HeaderOrder) > 0 {
		return &HeaderOrderingConn{
			Conn:        wrappedConn,
			OrderedKeys: cfg.HeaderOrder,
		}, nil
	}

	return wrappedConn, nil
}

func handshakeStandardTLS(
	ctx context.Context,
	conn net.Conn,
	host string,
	cfg *DialConfig,
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

	if cfg.InsecureSkipVerify {
		cloned := tlsCfg.Clone()
		cloned.InsecureSkipVerify = true
		tlsCfg = cloned
	}

	if len(cfg.ALPNOverride) > 0 {
		cloned := tlsCfg.Clone()
		cloned.NextProtos = cfg.ALPNOverride
		tlsCfg = cloned
	}

	if len(cfg.CertificatePins) > 0 {
		cloned := tlsCfg.Clone()
		if cloned.InsecureSkipVerify || net.ParseIP(host) != nil {
			cloned.InsecureSkipVerify = true
			cloned.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return netdial.VerifyCertificatePins(host, cfg.CertificatePins, rawCerts)
			}
		} else {
			origVerify := cloned.VerifyPeerCertificate

			//nolint:gosec // VerifyPeerCertificate is chained to enforce dynamic domain certificate pinning.
			cloned.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				if origVerify != nil {
					if err := origVerify(rawCerts, verifiedChains); err != nil {
						return err
					}
				}

				return netdial.VerifyCertificatePins(host, cfg.CertificatePins, rawCerts)
			}
		}

		tlsCfg = cloned
	}

	if tlsCfg.ServerName == "" && !tlsCfg.InsecureSkipVerify && tlsCfg.VerifyPeerCertificate == nil {
		cloned := tlsCfg.Clone()
		cloned.ServerName = host
		tlsCfg = cloned
	}

	if tlsCfg.VerifyPeerCertificate != nil && tlsCfg.ServerName == "" && !tlsCfg.InsecureSkipVerify {
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
