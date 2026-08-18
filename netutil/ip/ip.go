// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ip provides local IP address rotation, network interface discovery, and IPv6 subnet randomization.
package ip

import (
	"net"

	foundationip "github.com/lemon4ksan/foundation/net/ip"
)

var (
	// ErrEmptyIPPool is returned when initializing an IP rotator with an empty address pool.
	ErrEmptyIPPool = foundationip.ErrEmptyIPPool

	// ErrInvalidCIDR is returned when an invalid CIDR notation string is provided.
	ErrInvalidCIDR = foundationip.ErrInvalidCIDR
)

// DiscoverInterfaceIPs queries active system network interfaces and returns non-loopback IP addresses eligible for socket binding.
func DiscoverInterfaceIPs() ([]net.IP, error) {
	return foundationip.DiscoverInterfaceIPs()
}

// IsPrivateIP reports whether ip belongs to private (RFC 1918/4193), loopback, link-local, or CGNAT ranges.
func IsPrivateIP(ip net.IP) bool {
	return foundationip.IsPrivateIP(ip)
}

// SourceIPRotator maintains a pool of local IP addresses and cycles through them for socket binding.
type SourceIPRotator = foundationip.SourceIPRotator

// NewSourceIPRotator instantiates a [SourceIPRotator] with parsed local network addresses.
func NewSourceIPRotator(addrs []string) (*SourceIPRotator, error) {
	return foundationip.NewSourceIPRotator(addrs)
}

// IPv6SubnetRotator generates cryptographically random IPv6 addresses from a CIDR subnet prefix.
type IPv6SubnetRotator = foundationip.IPv6SubnetRotator

// NewIPv6SubnetRotator instantiates an [IPv6SubnetRotator] for the target CIDR prefix.
func NewIPv6SubnetRotator(cidr string) (*IPv6SubnetRotator, error) {
	return foundationip.NewIPv6SubnetRotator(cidr)
}
