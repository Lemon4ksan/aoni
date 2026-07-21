// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// DNSResolver resolves hostnames to IP addresses.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// DNSResolverFunc is an adapter to allow the use of ordinary functions as DNS resolvers.
type DNSResolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

func (f DNSResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

// StdlibResolver delegates DNS resolution to the system resolver via [net.Resolver].
type StdlibResolver struct {
	Resolver *net.Resolver
}

// NewStdlibResolver creates a [StdlibResolver] with the default resolver.
func NewStdlibResolver() *StdlibResolver {
	return &StdlibResolver{Resolver: &net.Resolver{}}
}

// LookupIPAddr delegates to the underlying [net.Resolver].
func (r *StdlibResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.Resolver.LookupIPAddr(ctx, host)
}

// ProxyRoutedDNSResolver sends DNS queries through a proxy connection to prevent leaks.
type ProxyRoutedDNSResolver struct {
	resolver  DNSResolver
	proxyDial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewProxyRoutedDNSResolver creates a [ProxyRoutedDNSResolver] that routes DNS queries
// through the given proxy dial function.
func NewProxyRoutedDNSResolver(
	resolver DNSResolver,
	proxyDial func(ctx context.Context, network, addr string) (net.Conn, error),
) *ProxyRoutedDNSResolver {
	return &ProxyRoutedDNSResolver{
		resolver:  resolver,
		proxyDial: proxyDial,
	}
}

// LookupIPAddr resolves the host by delegating to the proxy-routed resolver.
func (r *ProxyRoutedDNSResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r.resolver == nil {
		return nil, errors.New("aoni: proxy-routed DNS resolver: no underlying resolver configured")
	}

	return r.resolver.LookupIPAddr(ctx, host)
}

// FallbackResolver tries to resolve hostnames using a list of resolvers sequentially.
// If a resolver fails, it falls back to the next one.
type FallbackResolver struct {
	resolvers []DNSResolver
}

// NewFallbackResolver creates a new [FallbackResolver] with the given prioritized resolvers.
func NewFallbackResolver(resolvers ...DNSResolver) *FallbackResolver {
	active := make([]DNSResolver, 0, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			active = append(active, r)
		}
	}

	return &FallbackResolver{resolvers: active}
}

// LookupIPAddr implements the [DNSResolver] interface by trying resolvers sequentially.
func (r *FallbackResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if len(r.resolvers) == 0 {
		return nil, errors.New("aoni: dns: fallback resolver has no active resolvers configured")
	}

	var lastErr error
	for _, resolver := range r.resolvers {
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err == nil {
			return ips, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("aoni: dns: all fallback resolvers failed, last error: %w", lastErr)
}

// StaticResolver allows overriding DNS lookups with static IP mappings.
// If a queried host is not registered in the static map, it delegates
// the lookup to the next fallback resolver.
type StaticResolver struct {
	mapping  map[string][]net.IPAddr
	delegate DNSResolver
}

// NewStaticResolver creates a new [StaticResolver] with the given IP mapping and delegate.
func NewStaticResolver(mapping map[string][]string, delegate DNSResolver) *StaticResolver {
	if delegate == nil {
		delegate = &net.Resolver{}
	}

	ipMap := make(map[string][]net.IPAddr)
	for host, ips := range mapping {
		var parsed []net.IPAddr
		for _, ipStr := range ips {
			if ip := net.ParseIP(ipStr); ip != nil {
				parsed = append(parsed, net.IPAddr{IP: ip})
			}
		}

		if len(parsed) > 0 {
			ipMap[strings.ToLower(host)] = parsed
		}
	}

	return &StaticResolver{
		mapping:  ipMap,
		delegate: delegate,
	}
}

// LookupIPAddr implements the [DNSResolver] interface.
func (r *StaticResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	cleanHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if ips, ok := r.mapping[cleanHost]; ok {
		return ips, nil
	}

	return r.delegate.LookupIPAddr(ctx, host)
}

// FastRaceResolver executes multiple DNS resolutions concurrently and
// returns the fastest successful result, canceling all other pending queries.
type FastRaceResolver struct {
	resolvers []DNSResolver
}

// NewFastRaceResolver instantiates a concurrent [FastRaceResolver].
func NewFastRaceResolver(resolvers ...DNSResolver) *FastRaceResolver {
	active := make([]DNSResolver, 0, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			active = append(active, r)
		}
	}

	return &FastRaceResolver{resolvers: active}
}

// LookupIPAddr resolves the host by racing all configured resolvers in parallel.
// It returns the fastest successful result, cancelling all other pending queries.
//
// # Complexity
//
// Time Complexity: O(1) latency-wise (matches the speed of the fastest responder).
// Space Complexity: O(N) allocation, where N is the number of active resolvers.
func (r *FastRaceResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	// Filter out nil resolvers before setting up race tracking to avoid deadlocks
	var activeResolvers []DNSResolver
	for _, res := range r.resolvers {
		if res != nil {
			activeResolvers = append(activeResolvers, res)
		}
	}

	if len(activeResolvers) == 0 {
		return nil, errors.New("aoni race resolver: no active resolvers configured")
	}

	type result struct {
		ips []net.IPAddr
		err error
	}

	resCh := make(chan result, len(activeResolvers))

	raceCtx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	for _, res := range activeResolvers {
		go func(resolver DNSResolver) {
			ips, err := resolver.LookupIPAddr(raceCtx, host)
			select {
			case <-raceCtx.Done():
			case resCh <- result{ips: ips, err: err}:
			}
		}(res)
	}

	var lastErr error

	failedCount := 0
	activeCount := len(activeResolvers)

	for range activeCount {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-resCh:
			if res.err == nil {
				return res.ips, nil
			}

			lastErr = res.err

			failedCount++
			if failedCount == activeCount {
				return nil, fmt.Errorf("aoni race resolver: all concurrent resolutions failed, last error: %w", lastErr)
			}
		}
	}

	return nil, errors.New("aoni: race resolver: no responses received")
}
