// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"github.com/lemon4ksan/aoni/pipeline"
)

// ApplyRequestOverrides merges request-scoped pipeline overrides ([pipeline.RequestConfig])
// into an active [DialConfig] DTO, eliminating duplicate transport resolution logic.
func (cfg *DialConfig) ApplyRequestOverrides(reqCfg *pipeline.RequestConfig) {
	if reqCfg == nil {
		return
	}

	if reqCfg.Network != "" {
		cfg.Network = reqCfg.Network
	}

	if reqCfg.ProxyAddr != nil {
		cfg.ProxyURL = reqCfg.ProxyAddr
	}

	if reqCfg.DNSResolver != nil {
		cfg.DNSResolver = reqCfg.DNSResolver
	}

	if reqCfg.SocketController != nil {
		cfg.SocketController = reqCfg.SocketController
	}

	if reqCfg.Fragment != nil {
		cfg.FragmentConfig = reqCfg.Fragment
	}

	if reqCfg.HostRewrite != nil {
		cfg.HostRewriteRules = reqCfg.HostRewrite.Rules
	}
}
