// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"github.com/lemon4ksan/aoni/tunnel/ssh/client"
)

var (
	WithDialer                  = client.WithDialer
	WithPassword                = client.WithPassword
	WithKeyboardInteractive     = client.WithKeyboardInteractive
	WithKeyFile                 = client.WithKeyFile
	WithKey                     = client.WithKey
	WithCertFile                = client.WithCertFile
	WithCert                    = client.WithCert
	WithCertSigner              = client.WithCertSigner
	WithAgent                   = client.WithAgent
	WithAgentSocket             = client.WithAgentSocket
	WithDefaultAgent            = client.WithDefaultAgent
	WithAuth                    = client.WithAuth
	WithSigner                  = client.WithSigner
	WithPort                    = client.WithPort
	WithTimeout                 = client.WithTimeout
	WithWindowSize              = client.WithWindowSize
	WithMaxPacketSize           = client.WithMaxPacketSize
	WithHighPerformanceDefaults = client.WithHighPerformanceDefaults
	WithKnownHosts              = client.WithKnownHosts
	WithEnsureKnownHosts        = client.WithEnsureKnownHosts
	WithFingerprint             = client.WithFingerprint
	WithLegacyCiphers           = client.WithLegacyCiphers
	WithCiphers                 = client.WithCiphers
	WithKeyExchanges            = client.WithKeyExchanges
	WithRequestPty              = client.WithRequestPty
	WithPtyTerminal             = client.WithPtyTerminal
	WithInsecureIgnoreHostKey   = client.WithInsecureIgnoreHostKey
	WithHostKeyCallback         = client.WithHostKeyCallback
	WithHostKeyAlgorithms       = client.WithHostKeyAlgorithms
	WithBannerCallback          = client.WithBannerCallback
	WithProxy                   = client.WithProxy
	WithJump                    = client.WithJump
	WithConfig                  = client.WithConfig
	WithPath                    = client.WithPath
	WithStdout                  = client.WithStdout
	WithStderr                  = client.WithStderr
	WithStdin                   = client.WithStdin
	WithEnv                     = client.WithEnv
)
