// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

const (
	// DoHMediaType specifies the official IETF RFC 8484 media type for DoH queries and responses.
	DoHMediaType = "application/dns-message"
)

// DoHMethod specifies the HTTP method used for DoH queries (GET or POST per RFC 8484).
type DoHMethod int

const (
	// DoHMethodPost uses HTTP POST with raw wire format body (RFC 8484 Section 4.1).
	DoHMethodPost DoHMethod = iota

	// DoHMethodGet uses HTTP GET with base64url-encoded ?dns= parameter (RFC 8484 Section 4.1).
	DoHMethodGet
)

// DoHResolver resolves DNS via HTTPS using RFC 1035 wire format and RFC 8484 DoH specifications.
type DoHResolver struct {
	Endpoint string    // e.g. "https://1.1.1.1/dns-query"
	Host     string    // Host header override, e.g. "cloudflare-dns.com"
	Method   DoHMethod // DoHMethodPost or DoHMethodGet
	doer     aoni.RequestDoer
}

// NewDoHResolver creates a new RFC 8484 compliant DoHResolver.
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
		Method:   DoHMethodPost,
		doer:     engine,
	}
}

// LookupIPAddr queries A and AAAA records via RFC 8484 DoH using RFC 1035 wire format.
func (r *DoHResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	addrs, err := r.LookupNetIP(ctx, host)
	if err != nil {
		return nil, err
	}

	ipAddrs := make([]net.IPAddr, len(addrs))
	for i, a := range addrs {
		ipAddrs[i] = net.IPAddr{IP: a.AsSlice()}
	}

	return ipAddrs, nil
}

// LookupNetIP queries A and AAAA records returning zero-alloc netip.Addr structures.
func (r *DoHResolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	v4Addrs, err4 := r.queryWire(ctx, host, TypeA)
	v6Addrs, err6 := r.queryWire(ctx, host, TypeAAAA)

	if err4 != nil && err6 != nil {
		return nil, wrapDNSError(host, "DoH", r.Endpoint, err4)
	}

	return append(v4Addrs, v6Addrs...), nil
}

func (r *DoHResolver) queryWire(ctx context.Context, host string, qtype uint16) ([]netip.Addr, error) {
	var idBuf [2]byte

	_, _ = rand.Read(idBuf[:])
	queryID := binary.BigEndian.Uint16(idBuf[:])

	wireQuery, err := PackDNSQuery(queryID, host, qtype)
	if err != nil {
		return nil, err
	}

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetHeader("Accept", DoHMediaType)

	if r.Host != "" {
		req.SetHeader("Host", r.Host)
	}

	if r.Method == DoHMethodGet {
		encoded := base64.RawURLEncoding.EncodeToString(wireQuery)

		req.SetMethod(http.MethodGet)
		req.SetURL(r.Endpoint + "?dns=" + encoded)
	} else {
		req.SetMethod(http.MethodPost)
		req.SetURL(r.Endpoint)
		req.SetHeader("Content-Type", DoHMediaType)
		req.SetBodyBytes(wireQuery)
	}

	resp, err := r.doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("aoni doh: http status %d", resp.StatusCode())
	}

	return ParseDNSResponse(resp.BodyBytes(), queryID)
}
