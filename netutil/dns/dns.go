// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dns provides secure, resilient, and anti-censorship DNS resolution strategies.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
)

// ErrNoResolversConfigured is returned when a fallback or race resolver is instantiated without active resolvers.
var ErrNoResolversConfigured = errors.New("aoni: dns: no active resolvers configured")

// Resolver defines the hostname-to-IP lookup interface.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ResolverFunc adapts a function to the [Resolver] interface.
type ResolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

// LookupIPAddr executes the underlying function.
func (f ResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

// StdlibResolver delegates DNS resolutions directly to standard library [net.Resolver].
type StdlibResolver struct {
	Resolver *net.Resolver
}

// NewStdlibResolver instantiates a [StdlibResolver] wrapping standard system resolvers.
func NewStdlibResolver() *StdlibResolver {
	return &StdlibResolver{Resolver: &net.Resolver{}}
}

// LookupIPAddr delegates to the underlying [net.Resolver].
func (r *StdlibResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.Resolver.LookupIPAddr(ctx, host)
}

// ProxyRoutedResolver routes DNS query connections through proxy dialers to prevent local DNS leakage.
type ProxyRoutedResolver struct {
	resolver  Resolver
	proxyDial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewProxyRoutedDNSResolver creates a [ProxyRoutedResolver] routing lookups via proxyDial.
func NewProxyRoutedDNSResolver(
	resolver Resolver,
	proxyDial func(ctx context.Context, network, addr string) (net.Conn, error),
) *ProxyRoutedResolver {
	return &ProxyRoutedResolver{
		resolver:  resolver,
		proxyDial: proxyDial,
	}
}

// LookupIPAddr executes proxy-routed host resolution.
func (r *ProxyRoutedResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r.resolver == nil {
		return nil, ErrNoResolversConfigured
	}

	return r.resolver.LookupIPAddr(ctx, host)
}

// FallbackResolver attempts resolution across a prioritized list of resolvers sequentially.
type FallbackResolver struct {
	resolvers []Resolver
}

// NewFallbackResolver creates a [FallbackResolver] with active fallback resolvers.
func NewFallbackResolver(resolvers ...Resolver) *FallbackResolver {
	active := make([]Resolver, 0, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			active = append(active, r)
		}
	}

	return &FallbackResolver{resolvers: active}
}

// LookupIPAddr tries resolvers sequentially, returning the first successful response.
func (r *FallbackResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if len(r.resolvers) == 0 {
		return nil, ErrNoResolversConfigured
	}

	var lastErr error
	for _, resolver := range r.resolvers {
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err == nil {
			return ips, nil
		}

		lastErr = err
	}

	return nil, errors.New("aoni: dns: all fallback resolvers failed: " + lastErr.Error())
}

// StaticResolver allows overriding DNS resolutions with explicit static IP mappings.
type StaticResolver struct {
	mapping  map[string][]net.IPAddr
	delegate Resolver
}

// NewStaticResolver creates a [StaticResolver] with host-to-IP overrides and delegate fallbacks.
func NewStaticResolver(mapping map[string][]string, delegate Resolver) *StaticResolver {
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

// LookupIPAddr returns static IP mappings or delegates the lookup to the fallback resolver.
func (r *StaticResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	cleanHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if ips, ok := r.mapping[cleanHost]; ok {
		return ips, nil
	}

	return r.delegate.LookupIPAddr(ctx, host)
}

// FastRaceResolver races multiple DNS resolutions concurrently and returns the fastest successful result.
type FastRaceResolver struct {
	resolvers []Resolver
}

// NewFastRaceResolver instantiates a concurrent [FastRaceResolver].
func NewFastRaceResolver(resolvers ...Resolver) *FastRaceResolver {
	active := make([]Resolver, 0, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			active = append(active, r)
		}
	}

	return &FastRaceResolver{resolvers: active}
}

// LookupIPAddr races all configured resolvers concurrently, yielding the fastest response.
func (r *FastRaceResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	activeResolvers := generic.Filter(r.resolvers, func(res Resolver) bool { return res != nil })
	if len(activeResolvers) == 0 {
		return nil, ErrNoResolversConfigured
	}

	tasks := make([]func(context.Context) generic.Result[[]net.IPAddr], len(activeResolvers))
	for i, res := range activeResolvers {
		resolver := res
		tasks[i] = func(reqCtx context.Context) generic.Result[[]net.IPAddr] {
			ips, err := resolver.LookupIPAddr(reqCtx, host)
			if err != nil {
				return generic.Failure[[]net.IPAddr](err)
			}

			return generic.Success(ips)
		}
	}

	res := generic.RaceFirstSuccess(ctx, tasks...)
	if !res.IsSuccess() {
		_, err := res.Unwrap()
		return nil, fmt.Errorf("aoni/dns: all concurrent resolutions failed: %w", err)
	}

	ips, _ := res.Unwrap()

	return ips, nil
}

// LookupIPAddrResult executes a hostname lookup and returns a Swift-inspired [generic.Result].
func LookupIPAddrResult(ctx context.Context, r Resolver, host string) generic.Result[[]net.IPAddr] {
	if r == nil {
		return generic.Failure[[]net.IPAddr](ErrNoResolversConfigured)
	}

	ips, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return generic.Failure[[]net.IPAddr](err)
	}

	return generic.Success(ips)
}

// LookupFirstIP returns the first resolved IP wrapped in a [generic.Optional].
func LookupFirstIP(ctx context.Context, r Resolver, host string) generic.Optional[net.IP] {
	res := LookupIPAddrResult(ctx, r, host)
	if !res.IsSuccess() {
		return generic.None[net.IP]()
	}

	ips, _ := res.Unwrap()
	if len(ips) == 0 || ips[0].IP == nil {
		return generic.None[net.IP]()
	}

	return generic.Some(ips[0].IP)
}
