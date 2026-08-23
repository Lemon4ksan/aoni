// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.27

package handshake

import (
	"crypto/tls"
	"net"
)

func setupConfigForClient(conf *tls.Config) *tls.Config {
	return conf
}

func setupConfigForServer(conf *tls.Config, _, _ net.Addr) *tls.Config {
	return conf
}

func getQUICConfig(tlsConf *tls.Config, localAddr, remoteAddr net.Addr) *tls.QUICConfig {
	return &tls.QUICConfig{
		TLSConfig:           tlsConf,
		EnableSessionEvents: true,
		ClientHelloInfoConn: &conn{localAddr: localAddr, remoteAddr: remoteAddr},
	}
}
