// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	fdns "github.com/lemon4ksan/foundation/net/dns"
	"github.com/lemon4ksan/foundation/net/dns/wire"
	"github.com/lemon4ksan/foundation/silicon/rand"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/netutil/svcb"
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
// Defaults to a 5-second timeout [fast.Client] engine if doer is nil.
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

// LookupDNSRecords queries A and AAAA records concurrently over DoH, returning DNS records with authoritative TTLs.
func (r *DoHResolver) LookupDNSRecords(ctx context.Context, host string) ([]wire.DNSRecord, error) {
	var (
		v4Records, v6Records []wire.DNSRecord
		err4, err6           error
		wg                   sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()

		v4Records, err4 = r.queryWire(ctx, host, wire.TypeA)
	}()
	go func() {
		defer wg.Done()

		v6Records, err6 = r.queryWire(ctx, host, wire.TypeAAAA)
	}()

	wg.Wait()

	if err4 != nil && err6 != nil {
		return nil, fdns.WrapDNSError(host, "DoH", r.Endpoint, err4)
	}

	records := make([]wire.DNSRecord, 0, len(v4Records)+len(v6Records))
	records = append(records, v4Records...)
	records = append(records, v6Records...)

	return records, nil
}

// LookupHTTPS queries HTTPS resource records (RFC 9460 Type 65) over DoH.
func (r *DoHResolver) LookupHTTPS(ctx context.Context, host string, port uint16) ([]*svcb.Record, error) {
	qname := svcb.BuildHTTPSQueryName(host, port)

	wireBytes, err := r.LookupWireRecord(ctx, qname, svcb.TypeHTTPS)
	if err != nil {
		return nil, fdns.WrapDNSError(host, "DoH", r.Endpoint, err)
	}

	return svcb.ParseResponseRecords(wireBytes, svcb.TypeHTTPS)
}

// LookupSVCB queries general-purpose SVCB resource records (RFC 9460 Type 64) over DoH.
func (r *DoHResolver) LookupSVCB(ctx context.Context, scheme, service string, port uint16) ([]*svcb.Record, error) {
	qname := svcb.BuildSVCBQueryName(scheme, service, port)

	wireBytes, err := r.LookupWireRecord(ctx, qname, svcb.TypeSVCB)
	if err != nil {
		return nil, fdns.WrapDNSError(service, "DoH", r.Endpoint, err)
	}

	return svcb.ParseResponseRecords(wireBytes, svcb.TypeSVCB)
}

// LookupWireRecord queries a raw DNS wire format response over DoH for a specific query type.
func (r *DoHResolver) LookupWireRecord(ctx context.Context, host string, qtype uint16) ([]byte, error) {
	queryID := uint16(rand.Uint32())

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

	if r.Method == DoHMethodGet {
		req.SetMethod(http.MethodGet)
		req.SetURL(buildDoHGetURL(r.Endpoint, wireQuery))
	} else {
		req.SetMethod(http.MethodPost)
		req.SetURL(r.Endpoint)
		req.SetHeader("Content-Type", DoHMediaType)
		req.SetBodyBytes(wireQuery)
	}

	if r.Host != "" {
		req.SetHeader("Host", r.Host)
	}

	resp, err := r.doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("aoni/doh: http status %d", resp.StatusCode())
	}

	return resp.BodyBytes(), nil
}

func (r *DoHResolver) queryWire(ctx context.Context, host string, qtype uint16) ([]wire.DNSRecord, error) {
	queryID := uint16(rand.Uint32())

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

	if r.Method == DoHMethodGet {
		req.SetMethod(http.MethodGet)
		req.SetURL(buildDoHGetURL(r.Endpoint, wireQuery))
	} else {
		req.SetMethod(http.MethodPost)
		req.SetURL(r.Endpoint)
		req.SetHeader("Content-Type", DoHMediaType)
		req.SetBodyBytes(wireQuery)
	}

	if r.Host != "" {
		req.SetHeader("Host", r.Host)
	}

	resp, err := r.doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("aoni/doh: http status %d", resp.StatusCode())
	}

	return wire.ParseDNSResponseRecords(resp.BodyBytes(), queryID)
}

func buildDoHGetURL(endpoint string, wireQuery []byte) string {
	encodedLen := base64.RawURLEncoding.EncodedLen(len(wireQuery))
	prefixLen := len(endpoint) + len("?dns=")
	totalLen := prefixLen + encodedLen

	var (
		stackBuf [1024]byte
		b        []byte
	)

	if totalLen <= len(stackBuf) {
		b = stackBuf[:totalLen]
	} else {
		b = make([]byte, totalLen)
	}

	copy(b, endpoint)
	copy(b[len(endpoint):], "?dns=")
	base64.RawURLEncoding.Encode(b[prefixLen:], wireQuery)

	return string(b)
}
