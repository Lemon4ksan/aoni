// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy_test

import (
	"net"
	"net/url"
	"testing"

	"github.com/lemon4ksan/aoni/netutil/proxy"
)

func TestParsePACResult(t *testing.T) {
	tests := []struct {
		input     string
		wantTypes []string
		wantHosts []string
	}{
		{
			input:     "DIRECT",
			wantTypes: []string{"DIRECT"},
			wantHosts: []string{""},
		},
		{
			input:     "PROXY proxy.corp:8080; SOCKS5 socks.corp:1080; DIRECT",
			wantTypes: []string{"PROXY", "SOCKS5", "DIRECT"},
			wantHosts: []string{"proxy.corp:8080", "socks.corp:1080", ""},
		},
		{
			input:     "HTTPS secure.proxy.com:443",
			wantTypes: []string{"HTTPS"},
			wantHosts: []string{"secure.proxy.com:443"},
		},
		{
			input:     "",
			wantTypes: []string{"DIRECT"},
			wantHosts: []string{""},
		},
	}

	for _, tt := range tests {
		directives := proxy.ParsePACResult(tt.input)
		if len(directives) != len(tt.wantTypes) {
			t.Fatalf("ParsePACResult(%q) got %d directives, want %d", tt.input, len(directives), len(tt.wantTypes))
		}

		for i, d := range directives {
			if d.Type != tt.wantTypes[i] {
				t.Errorf("[%d] got Type %q, want %q", i, d.Type, tt.wantTypes[i])
			}

			if d.HostPort != tt.wantHosts[i] {
				t.Errorf("[%d] got HostPort %q, want %q", i, d.HostPort, tt.wantHosts[i])
			}
		}
	}
}

func TestPACEngine_Rules(t *testing.T) {
	_, corpNet, _ := net.ParseCIDR("10.0.0.0/8")

	engine := proxy.NewPACEngine("DIRECT")
	engine.AddRule(proxy.PACRule{
		HostSuffix: ".internal.corp",
		Result:     "DIRECT",
	})
	engine.AddRule(proxy.PACRule{
		Subnet: corpNet,
		Result: "DIRECT",
	})
	engine.AddRule(proxy.PACRule{
		Pattern: "*blocked.com*",
		Result:  "PROXY gateway.corp:8080; SOCKS5 socks.corp:1080",
	})

	// 1. Internal host -> DIRECT
	d := engine.FindProxyForURL("https://api.internal.corp/v1", "api.internal.corp")
	if len(d) == 0 || d[0].Type != "DIRECT" {
		t.Fatalf("expected DIRECT for internal.corp, got %v", d)
	}

	// 2. Blocked pattern -> PROXY
	d = engine.FindProxyForURL("https://blocked.com/index.html", "blocked.com")
	if len(d) != 2 || d[0].Type != "PROXY" || d[0].HostPort != "gateway.corp:8080" {
		t.Fatalf("expected PROXY gateway.corp:8080, got %v", d)
	}

	// 3. Unmatched -> Default DIRECT
	d = engine.FindProxyForURL("https://google.com/", "google.com")
	if len(d) == 0 || d[0].Type != "DIRECT" {
		t.Fatalf("expected default DIRECT, got %v", d)
	}

	// 4. ProxyURLFunc integration
	fn := engine.ProxyURLFunc()
	u, _ := url.Parse("https://blocked.com/test")

	proxyURL, err := fn(u)
	if err != nil {
		t.Fatalf("unexpected error from ProxyURLFunc: %v", err)
	}

	if proxyURL == nil || proxyURL.String() != "http://gateway.corp:8080" {
		t.Fatalf("expected http://gateway.corp:8080, got %v", proxyURL)
	}
}

func TestPACHelpers(t *testing.T) {
	if !proxy.IsPlainHostName("intranet") {
		t.Error("expected intranet to be plain")
	}

	if proxy.IsPlainHostName("intranet.corp") {
		t.Error("expected intranet.corp not to be plain")
	}

	if !proxy.DNSDomainIs("api.google.com", ".google.com") {
		t.Error("expected api.google.com to match .google.com")
	}

	if !proxy.LocalHostOrDomainIs("intranet", "intranet.corp") {
		t.Error("expected intranet to match intranet.corp")
	}

	if proxy.DNSDomainLevels("a.b.c.com") != 3 {
		t.Errorf("expected 3 dots, got %d", proxy.DNSDomainLevels("a.b.c.com"))
	}

	if !proxy.IsInNet("192.168.1.50", "192.168.1.0", "255.255.255.0") {
		t.Error("expected 192.168.1.50 in 192.168.1.0/24")
	}

	if proxy.IsInNet("10.0.0.1", "192.168.1.0", "255.255.255.0") {
		t.Error("expected 10.0.0.1 not in 192.168.1.0/24")
	}
}
