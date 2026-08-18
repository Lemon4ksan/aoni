// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/foundation/pipeline"
)

// ApplyRequestOverrides merges request-scoped pipeline overrides ([pipeline.RequestConfig])
// into an active [DialConfig] DTO, eliminating duplicate transport resolution logic.
func (cfg *DialConfig) ApplyRequestOverrides(reqCfg *pipeline.RequestConfig) {
	if reqCfg == nil {
		return
	}

	if reqCfg.ProxyAddr != nil {
		cfg.ProxyURL = reqCfg.ProxyAddr
	}

	if reqCfg.DNSResolver != nil {
		cfg.DNSResolver = reqCfg.DNSResolver
	}

	if reqCfg.P0fSignature != nil {
		cfg.P0fSignature = reqCfg.P0fSignature
	}

	if reqCfg.SocketController != nil {
		cfg.SocketController = reqCfg.SocketController
	}

	if reqCfg.Fragment != nil {
		cfg.FragmentConfig = reqCfg.Fragment
	}

	if reqCfg.ClientHelloSpecProvider != nil {
		cfg.SpecProvider = reqCfg.ClientHelloSpecProvider
	}

	if reqCfg.SessionCache != nil {
		cfg.SessionCache = reqCfg.SessionCache
	}

	if reqCfg.JA4Callback != nil {
		cfg.JA4Callback = reqCfg.JA4Callback
	}

	if len(reqCfg.ALPNOverride) > 0 {
		cfg.ALPNOverride = reqCfg.ALPNOverride
	}

	if reqCfg.JA4ReportStore != nil {
		if reqCfg.JA4ReportStore.Report == nil {
			reqCfg.JA4ReportStore.Report = &ja4.Report{}
		}

		if reqCfg.JA4ReportStore.Target != nil {
			reqCfg.JA4ReportStore.Target.JA4 = reqCfg.JA4ReportStore.Report
		}

		cfg.JA4ReportStore = reqCfg.JA4ReportStore.Report
	}

	if len(reqCfg.CertificatePins) > 0 {
		cfg.CertificatePins = reqCfg.CertificatePins
	}

	if len(reqCfg.OrderedHeaders) > 0 {
		cfg.HeaderOrder = reqCfg.OrderedHeaders
	}

	if reqCfg.HostRewrite != nil {
		cfg.HostRewriteRules = reqCfg.HostRewrite.Rules
	}
}
