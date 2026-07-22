// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// DoHResolver resolves DNS via HTTPS, supporting both A and AAAA records.
// Uses an isolated [http.Client] that connects directly to the DoH server by IP,
// bypassing the system resolver entirely to avoid circular DNS lookups.
type DoHResolver struct {
	Endpoint string // IP-based URL, e.g. "https://1.1.1.1/dns-query"
	Host     string // Host header override, e.g. "cloudflare-dns.com"

	client *http.Client
}

// NewDoHResolver creates a [DoHResolver] that queries the given IP-based endpoint.
// The endpoint should be an IP-based URL (e.g. "https://1.1.1.1/dns-query"),
// and host is the Host header value (e.g. "cloudflare-dns.com").
func NewDoHResolver(endpoint, host string) *DoHResolver {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 5 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2: true,
	}

	return &DoHResolver{
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
		Endpoint: endpoint,
		Host:     host,
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil) //nolint:gosec
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-json")

	if r.Host != "" {
		req.Host = r.Host
	}

	resp, err := r.client.Do(req) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp dohResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
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
