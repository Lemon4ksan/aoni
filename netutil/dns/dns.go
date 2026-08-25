// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dns provides secure, resilient, and anti-censorship DNS resolution strategies
// including DNS over HTTPS (DoH / RFC 8484), DNS over QUIC (DoQ / RFC 9250), DNS over TLS (DoT / RFC 7858),
// in-memory caching with serve-stale resilience (RFC 8767), and concurrent resolution racing.
package dns

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	fdns "github.com/lemon4ksan/foundation/net/dns"
)

// Standard DNS caching and resiliency constants defined in RFC 8767 and RFC 2308.
const (
	// MaxTTLCap is the maximum recommended authoritative TTL cap of 7 days (RFC 8767 §4).
	MaxTTLCap = fdns.MaxTTLCap

	// DefaultMaxStaleTTL is the recommended retention duration for expired records in cache (RFC 8767 §5).
	DefaultMaxStaleTTL = fdns.DefaultMaxStaleTTL

	// DefaultStaleResponseTTL is the recommended TTL to set on stale records in responses (RFC 8767 §4 & §6).
	DefaultStaleResponseTTL = fdns.DefaultStaleResponseTTL

	// DefaultClientResponseTimeout is the recommended time to wait for upstream before serving stale data (RFC 8767 §5).
	DefaultClientResponseTimeout = fdns.DefaultClientResponseTimeout

	// FailureRecheckInterval is the recommended cooldown between retrying failed authoritative lookups (RFC 8767 §5).
	FailureRecheckInterval = fdns.FailureRecheckInterval

	// DefaultNegativeTTL is the default duration for caching NXDOMAIN and NODATA negative responses (RFC 2308 §5).
	DefaultNegativeTTL = fdns.DefaultNegativeTTL

	// MaxNegativeTTLCap is the maximum recommended negative cache duration cap of 3 hours (RFC 2308 §5).
	MaxNegativeTTLCap = fdns.MaxNegativeTTLCap
)

// Standard DNS error sentinels re-exported from [foundation/net/dns].
var (
	// ErrNoResolversConfigured is returned when a fallback or race resolver is instantiated without active resolvers.
	ErrNoResolversConfigured = fdns.ErrNoResolversConfigured

	// ErrNXDomain indicates the queried domain name does not exist (RCODE 3 / RFC 2308 §2.1).
	ErrNXDomain = fdns.ErrNXDomain

	// ErrNODATA indicates the queried domain name exists but has no records of the requested type (RFC 2308 §2.2).
	ErrNODATA = fdns.ErrNODATA
)

// Resolver defines the hostname-to-IP lookup interface.
type Resolver = fdns.Resolver

// ResolverFunc adapts a function to the [Resolver] interface.
type ResolverFunc = fdns.ResolverFunc

// StdlibResolver delegates DNS resolutions directly to standard library [net.Resolver].
type StdlibResolver = fdns.StdlibResolver

// NewStdlibResolver instantiates a [StdlibResolver] wrapping standard system resolvers.
func NewStdlibResolver() *StdlibResolver {
	return fdns.NewStdlibResolver()
}

// ProxyRoutedResolver routes DNS query connections through proxy dialers to prevent local DNS leakage.
type ProxyRoutedResolver = fdns.ProxyRoutedResolver

// NewProxyRoutedDNSResolver creates a [ProxyRoutedResolver] routing lookups via proxyDial.
func NewProxyRoutedDNSResolver(
	resolver Resolver,
	proxyDial func(ctx context.Context, network, addr string) (net.Conn, error),
) *ProxyRoutedResolver {
	return fdns.NewProxyRoutedDNSResolver(resolver, proxyDial)
}

// FallbackResolver attempts resolution across a prioritized list of resolvers sequentially.
type FallbackResolver = fdns.FallbackResolver

// NewFallbackResolver creates a [FallbackResolver] with active fallback resolvers.
func NewFallbackResolver(resolvers ...Resolver) *FallbackResolver {
	return fdns.NewFallbackResolver(resolvers...)
}

// StaticResolver allows overriding DNS resolutions with explicit static IP mappings.
type StaticResolver = fdns.StaticResolver

// NewStaticResolver creates a [StaticResolver] with host-to-IP overrides and delegate fallbacks.
func NewStaticResolver(mapping map[string][]string, delegate Resolver) *StaticResolver {
	return fdns.NewStaticResolver(mapping, delegate)
}

// FastRaceResolver races multiple DNS resolutions concurrently and returns the fastest successful result.
type FastRaceResolver = fdns.FastRaceResolver

// NewFastRaceResolver instantiates a concurrent [FastRaceResolver].
func NewFastRaceResolver(resolvers ...Resolver) *FastRaceResolver {
	return fdns.NewFastRaceResolver(resolvers...)
}

// DoTResolver resolves DNS over TLS (RFC 7858), querying both A and AAAA records.
type DoTResolver = fdns.DoTResolver

// NewDoTResolver creates a [DoTResolver] with the specified server endpoint and TLS hostname.
func NewDoTResolver(endpoint, host string) *DoTResolver {
	return fdns.NewDoTResolver(endpoint, host)
}

// InMemoryDNSCache manages thread-safe, in-memory caching of DNS resolutions.
type InMemoryDNSCache = fdns.InMemoryDNSCache

// CacheOption configures an [InMemoryDNSCache].
type CacheOption = fdns.CacheOption

// WithServeStale enables or disables serving stale DNS records on upstream resolution failures (RFC 8767).
func WithServeStale(enabled bool) CacheOption {
	return fdns.WithServeStale(enabled)
}

// WithNegativeCaching enables or disables negative response caching (RFC 2308).
func WithNegativeCaching(enabled bool) CacheOption {
	return fdns.WithNegativeCaching(enabled)
}

// WithNegativeTTL configures the caching duration for negative responses (RFC 2308 §5).
func WithNegativeTTL(d time.Duration) CacheOption {
	return fdns.WithNegativeTTL(d)
}

// WithMaxStaleTTL configures the retention duration for expired records in cache (RFC 8767 §5).
func WithMaxStaleTTL(d time.Duration) CacheOption {
	return fdns.WithMaxStaleTTL(d)
}

// WithClientResponseTimeout sets the timeout before returning stale data while upstream lookup continues (RFC 8767 §5).
func WithClientResponseTimeout(d time.Duration) CacheOption {
	return fdns.WithClientResponseTimeout(d)
}

// NewInMemoryDNSCache creates an InMemoryDNSCache and launches background cache eviction.
func NewInMemoryDNSCache(ttl time.Duration, r Resolver, opts ...CacheOption) *InMemoryDNSCache {
	return fdns.NewInMemoryDNSCache(ttl, r, opts...)
}

// IsNotFound reports whether err represents an authoritative negative DNS response (NXDOMAIN or NODATA per RFC 2308).
func IsNotFound(err error) bool {
	return fdns.IsNotFound(err)
}

// IsNXDomain reports whether err represents an authoritative NXDOMAIN Name Error (RFC 8020 / RFC 2308 §2.1).
func IsNXDomain(err error) bool {
	return fdns.IsNXDomain(err)
}

// ResolutionError represents an error occurring during DNS resolution.
type ResolutionError = fdns.ResolutionError

// LookupIPAddrResult executes a hostname lookup and returns a [generic.Result].
func LookupIPAddrResult(ctx context.Context, r Resolver, host string) generic.Result[[]net.IPAddr] {
	return fdns.LookupIPAddrResult(ctx, r, host)
}

// LookupFirstIP returns the first resolved IP wrapped in a [generic.Optional].
func LookupFirstIP(ctx context.Context, r Resolver, host string) generic.Optional[net.IP] {
	return fdns.LookupFirstIP(ctx, r, host)
}

// LookupNetIP resolves a hostname into a slice of zero-allocation [netip.Addr] value objects.
func LookupNetIP(ctx context.Context, r Resolver, host string) ([]netip.Addr, error) {
	return fdns.LookupNetIP(ctx, r, host)
}

// LookupNetIPResult executes a hostname lookup and returns a [generic.Result] containing [netip.Addr] slices.
func LookupNetIPResult(ctx context.Context, r Resolver, host string) generic.Result[[]netip.Addr] {
	return fdns.LookupNetIPResult(ctx, r, host)
}

// LookupFirstNetIP returns the first resolved [netip.Addr] wrapped in a [generic.Optional].
func LookupFirstNetIP(ctx context.Context, r Resolver, host string) generic.Optional[netip.Addr] {
	return fdns.LookupFirstNetIP(ctx, r, host)
}
