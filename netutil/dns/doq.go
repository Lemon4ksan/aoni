// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/net/dns/wire"
	"github.com/quic-go/quic-go"
)

const (
	DoQALPN        = "doq"
	DoQDefaultPort = "853"
)

// DoQResolver resolves DNS queries over dedicated QUIC connections (RFC 9250),
// eliminating head-of-line blocking while providing TLS 1.3 transport encryption.
type DoQResolver struct {
	Endpoint   string
	Host       string
	Timeout    time.Duration
	EDNS       wire.EDNSOptions
	TLSConfig  *tls.Config
	Enable0RTT bool

	mu   sync.Mutex
	conn *quic.Conn
}

// NewDoQResolver constructs a DoQResolver targeting endpoint with TLS SNI host.
func NewDoQResolver(endpoint, host string) *DoQResolver {
	return &DoQResolver{
		Endpoint: endpoint,
		Host:     host,
		Timeout:  5 * time.Second,
		EDNS:     wire.EDNSOptions{PadToBlock: 128},
	}
}

// LookupIPAddr queries A and AAAA records over DoQ and returns IP addresses.
func (r *DoQResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	records, err := r.LookupDNSRecords(ctx, host)
	if err != nil {
		return nil, wrapDNSError(host, "DoQ", r.Endpoint, err)
	}

	addrs := make([]net.IPAddr, len(records))
	for i, rec := range records {
		addrs[i] = net.IPAddr{IP: rec.Addr.AsSlice()}
	}

	return addrs, nil
}

// LookupDNSRecords queries A and AAAA records concurrently over DoQ, returning DNS records with authoritative TTLs.
func (r *DoQResolver) LookupDNSRecords(ctx context.Context, host string) ([]wire.DNSRecord, error) {
	var (
		v4Records, v6Records []wire.DNSRecord
		err4, err6           error
		wg                   sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()

		v4Records, err4 = r.queryStreamRecords(ctx, host, wire.TypeA)
	}()
	go func() {
		defer wg.Done()

		v6Records, err6 = r.queryStreamRecords(ctx, host, wire.TypeAAAA)
	}()

	wg.Wait()

	if err4 != nil && err6 != nil {
		return nil, err4
	}

	records := make([]wire.DNSRecord, 0, len(v4Records)+len(v6Records))
	records = append(records, v4Records...)
	records = append(records, v6Records...)

	return records, nil
}

// LookupWireRecord queries a raw DNS wire format response over DoQ for a specific query type.
func (r *DoQResolver) LookupWireRecord(ctx context.Context, host string, qtype uint16) ([]byte, error) {
	qConn, err := r.getOrCreateConn(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := qConn.OpenStreamSync(ctx)
	if err != nil {
		r.closeConn()
		return nil, fmt.Errorf("%w: %w", ErrDoQStreamClosed, err)
	}
	defer stream.Close()

	edns := r.EDNS
	if edns.PadToBlock <= 0 {
		edns.PadToBlock = 128
	}

	wireQuery, err := wire.PackDNSQueryExtended(0, host, qtype, edns)
	if err != nil {
		return nil, err
	}

	if err := sendDoQQuery(stream, wireQuery); err != nil {
		return nil, err
	}

	return readDoQResponse(stream)
}

func (r *DoQResolver) queryStreamRecords(ctx context.Context, host string, qtype uint16) ([]wire.DNSRecord, error) {
	qConn, err := r.getOrCreateConn(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := qConn.OpenStreamSync(ctx)
	if err != nil {
		r.closeConn()
		return nil, fmt.Errorf("%w: %w", ErrDoQStreamClosed, err)
	}
	defer stream.Close()

	edns := r.EDNS
	if edns.PadToBlock <= 0 {
		edns.PadToBlock = 128
	}

	// RFC 9250 Section 4.2.1: DNS Message ID MUST be set to 0 over DoQ
	wireQuery, err := wire.PackDNSQueryExtended(0, host, qtype, edns)
	if err != nil {
		return nil, err
	}

	if err := sendDoQQuery(stream, wireQuery); err != nil {
		return nil, err
	}

	payload, err := readDoQResponse(stream)
	if err != nil {
		return nil, err
	}

	return wire.ParseDNSResponseRecords(payload, 0)
}

func sendDoQQuery(stream *quic.Stream, wireQuery []byte) error {
	var lengthBuf [2]byte
	binary.BigEndian.PutUint16(lengthBuf[:], uint16(len(wireQuery)))

	if _, err := stream.Write(lengthBuf[:]); err != nil {
		return err
	}

	if _, err := stream.Write(wireQuery); err != nil {
		return err
	}

	return stream.Close()
}

func readDoQResponse(stream *quic.Stream) ([]byte, error) {
	var lengthBuf [2]byte
	if _, err := io.ReadFull(stream, lengthBuf[:]); err != nil {
		return nil, ErrDoQStreamClosed
	}

	msgLen := binary.BigEndian.Uint16(lengthBuf[:])
	if msgLen == 0 {
		return nil, ErrDoQInvalidMessage
	}

	payload := make([]byte, msgLen)
	if _, err := io.ReadFull(stream, payload); err != nil {
		return nil, ErrDoQStreamClosed
	}

	return payload, nil
}

func (r *DoQResolver) getOrCreateConn(ctx context.Context) (*quic.Conn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil && r.conn.Context().Err() == nil {
		return r.conn, nil
	}

	tlsCfg := r.buildTLSConfig()
	quicCfg := &quic.Config{
		KeepAlivePeriod: 15 * time.Second,
	}

	endpoint := r.Endpoint
	if !hasPort(endpoint) {
		endpoint = net.JoinHostPort(endpoint, DoQDefaultPort)
	}

	conn, err := quic.DialAddr(ctx, endpoint, tlsCfg, quicCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDoQHandshakeFailed, err)
	}

	r.conn = conn

	return conn, nil
}

func (r *DoQResolver) buildTLSConfig() *tls.Config {
	var base *tls.Config
	if r.TLSConfig != nil {
		base = r.TLSConfig.Clone()
	} else {
		base = &tls.Config{MinVersion: tls.VersionTLS13}
	}

	base.NextProtos = []string{DoQALPN}
	if base.ServerName == "" && r.Host != "" {
		base.ServerName = r.Host
	}

	return base
}

func (r *DoQResolver) closeConn() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil {
		_ = r.conn.CloseWithError(0x0, "closing connection")
		r.conn = nil
	}
}

func hasPort(s string) bool {
	_, _, err := net.SplitHostPort(s)
	return err == nil
}
