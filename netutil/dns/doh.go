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
	"github.com/lemon4ksan/aoni/netutil/dns/wire"
	"github.com/lemon4ksan/aoni/option"
)

// DoHMediaType specifies the official IETF RFC 8484 media type for DoH queries and responses.
const DoHMediaType = "application/dns-message"

// DoHMethod specifies the HTTP method used for DoH queries (GET or POST per RFC 8484).
type DoHMethod int

const (
	// DoHMethodPost uses HTTP POST with raw wire format body (RFC 8484 Section 4.1).
	DoHMethodPost DoHMethod = iota

	// DoHMethodGet uses HTTP GET with base64url-encoded ?dns= parameter (RFC 8484 Section 4.1).
	DoHMethodGet
)

// DoHResolver resolves hostnames using DNS over HTTPS (RFC 8484)
// with support for EDNS0 Client Subnet (ECS, RFC 7871) and message padding (RFC 8467).
//
// Specification Adherence:
// Conforms strictly to IETF RFC 8484 (DNS Queries over HTTPS) and RFC 9460 (SVCB/HTTPS RRs).
//
// Thread Safety & Concurrency:
// 100% thread-safe; resolver instances are read-only after construction and safe for concurrent queries across goroutines.
type DoHResolver struct {
	Endpoint string
	Host     string
	Method   DoHMethod
	EDNS     wire.EDNSOptions
	doer     aoni.RequestDoer
}

// NewDoHResolver constructs a [DoHResolver] bound to endpoint and host.
// The doer parameter accepts any engine implementation (*fast.Client, *aoni.Client, *http.Client, or nil).
//
// Preconditions:
//   - endpoint must be a valid HTTP/HTTPS URL string.
//   - If doer is nil, defaults to a 5-second timeout [fast.Client] engine.
//
// Postconditions:
//   - Yields a non-nil, thread-safe [DoHResolver] pointer ready for hostname resolution.
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
		EDNS:     wire.EDNSOptions{PadToBlock: 128},
		doer:     engine,
	}
}

// LookupIPAddr queries A and AAAA records over DoH and returns IP addresses.
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

// LookupNetIP queries A and AAAA records over DoH and returns netip.Addr structures.
func (r *DoHResolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	records, err := r.LookupDNSRecords(ctx, host)
	if err != nil {
		return nil, err
	}

	addrs := make([]netip.Addr, len(records))
	for i, rec := range records {
		addrs[i] = rec.Addr
	}

	return addrs, nil
}

// LookupDNSRecords queries A and AAAA records over DoH, returning DNS records with authoritative TTLs.
func (r *DoHResolver) LookupDNSRecords(ctx context.Context, host string) ([]wire.DNSRecord, error) {
	v4Records, err4 := r.queryWire(ctx, host, wire.TypeA)
	v6Records, err6 := r.queryWire(ctx, host, wire.TypeAAAA)

	if err4 != nil && err6 != nil {
		return nil, wrapDNSError(host, "DoH", r.Endpoint, err4)
	}

	return append(v4Records, v6Records...), nil
}

// LookupWireRecord queries a raw DNS wire format response over DoH for a specific query type.
func (r *DoHResolver) LookupWireRecord(ctx context.Context, host string, qtype uint16) ([]byte, error) {
	var idBuf [2]byte

	_, _ = rand.Read(idBuf[:])
	queryID := binary.BigEndian.Uint16(idBuf[:])

	edns := r.EDNS
	if edns.PadToBlock <= 0 {
		edns.PadToBlock = 128
	}

	wireQuery, err := wire.PackDNSQueryExtended(queryID, host, qtype, edns)
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

	return resp.BodyBytes(), nil
}

func (r *DoHResolver) queryWire(ctx context.Context, host string, qtype uint16) ([]wire.DNSRecord, error) {
	var idBuf [2]byte

	_, _ = rand.Read(idBuf[:])
	queryID := binary.BigEndian.Uint16(idBuf[:])

	edns := r.EDNS
	if edns.PadToBlock <= 0 {
		edns.PadToBlock = 128
	}

	wireQuery, err := wire.PackDNSQueryExtended(queryID, host, qtype, edns)
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

	return wire.ParseDNSResponseRecords(resp.BodyBytes(), queryID)
}
