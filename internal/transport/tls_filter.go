// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"crypto/tls"
	"net"
)

// TLSHandshakeFilter is the codec filter for L7 TLS encryption and ALPN negotiation.
func TLSHandshakeFilter(ctx context.Context, conn net.Conn, targetHost string, cfg *DialConfig) (net.Conn, error) {
	if cfg == nil {
		return conn, nil
	}

	if cfg.WrapTLSClient != nil {
		baseCfg := cfg.BaseTLSConfig
		if baseCfg == nil {
			baseCfg = &tls.Config{}
		} else {
			baseCfg = baseCfg.Clone()
		}

		if cfg.InsecureSkipVerify {
			baseCfg.InsecureSkipVerify = true
		}

		return cfg.WrapTLSClient(ctx, conn, baseCfg, targetHost)
	}

	return handshakeStandardTLS(ctx, conn, targetHost, cfg)
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
	serverName := host

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

	if tlsCfg.ServerName == "" && !tlsCfg.InsecureSkipVerify && tlsCfg.VerifyPeerCertificate == nil {
		cloned := tlsCfg.Clone()
		cloned.ServerName = host
		tlsCfg = cloned
	}

	tlsConn := tls.Client(conn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return tlsConn, nil
}
