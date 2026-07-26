// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

// DoHResolver resolves DNS via HTTPS (A and AAAA records) using a universal HTTP execution engine.
type DoHResolver struct {
	Endpoint string // IP-based URL, e.g. "https://1.1.1.1/dns-query"
	Host     string // Host header override, e.g. "cloudflare-dns.com"

	doer aoni.RequestDoer
}

// NewDoHResolver creates a DoHResolver.
// The doer parameter accepts any client implementation (*fast.Client, *aoni.Client, *http.Client, or nil).
// If doer is nil, it defaults to a high-performance fast.Client.
func NewDoHResolver(endpoint, host string, doer any) *DoHResolver {
	var engine aoni.RequestDoer
	if doer == nil {
		engine = fast.NewClient(option.WithTimeout(5 * time.Second))
	} else {
		engine = aoni.Configure(doer)
	}

	return &DoHResolver{
		Endpoint: endpoint,
		Host:     host,
		doer:     engine,
	}
}

type dohResponse struct {
	Answer []dohAnswer `json:"Answer"`
}

type dohAnswer struct {
	Type int    `json:"type"` // 1 = A, 28 = AAAA
	Data string `json:"data"`
}

// LookupIPAddr queries both A and AAAA records via DoH.
func (r *DoHResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	// Query A records
	aIPs, err := r.query(ctx, host, 1)
	if err != nil {
		return nil, wrapDNSError(host, "DoH", r.Endpoint, err)
	}

	// Query AAAA records
	aaaaIPs, err := r.query(ctx, host, 28)
	if err != nil {
		return aIPs, nil //nolint:nilerr
	}

	return append(aIPs, aaaaIPs...), nil
}

func (r *DoHResolver) query(ctx context.Context, host string, qtype uint16) ([]net.IPAddr, error) {
	reqURL := fmt.Sprintf("%s?name=%s&type=%d", r.Endpoint, url.QueryEscape(host), qtype)

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetMethod(http.MethodGet)
	req.SetURL(reqURL)
	req.SetHeader("Accept", "application/dns-json")

	if r.Host != "" {
		req.SetHeader("Host", r.Host)
	}

	resp, err := r.doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	var apiResp dohResponse

	// Fast path: attempt zero-copy decoding if the response provides direct buffer access
	if unsafe, ok := resp.(interface{ UnsafeBodyBytes() []byte }); ok {
		if err := json.Unmarshal(unsafe.UnsafeBodyBytes(), &apiResp); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(resp.BodyBytes(), &apiResp); err != nil {
			return nil, err
		}
	}

	var ips []net.IPAddr
	for _, ans := range apiResp.Answer {
		if ans.Type == int(qtype) {
			if ip := net.ParseIP(ans.Data); ip != nil {
				ips = append(ips, net.IPAddr{IP: ip})
			}
		}
	}

	return ips, nil
}
