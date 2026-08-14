// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ip provides local IP address rotation, network interface discovery, and IPv6 subnet randomization.
package ip

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
)

var (
	// ErrEmptyIPPool is returned when initializing an IP rotator with an empty address pool.
	ErrEmptyIPPool = errors.New("aoni: source IP pool cannot be empty")

	// ErrInvalidCIDR is returned when an invalid CIDR notation string is provided.
	ErrInvalidCIDR = errors.New("netutil: invalid CIDR notation")
)

// DiscoverInterfaceIPs queries active system network interfaces and returns non-loopback IP addresses eligible for socket binding.
func DiscoverInterfaceIPs() ([]net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("netutil: query interfaces failed: %w", err)
	}

	var ips []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ip := extractIP(addr)
			if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
				ips = append(ips, ip)
			}
		}
	}

	return ips, nil
}

// IsPrivateIP reports whether ip belongs to private (RFC 1918/4193), loopback, link-local, or CGNAT ranges.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 0 ||
			ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168) ||
			(ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127)
	}

	if ip6 := ip.To16(); ip6 != nil {
		return (ip6[0] & 0xfe) == 0xfc
	}

	return false
}

func extractIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

// SourceIPRotator maintains a pool of local IP addresses and cycles through them for socket binding.
// The pool slice is stored in an [atomic.Pointer] using Copy-On-Write semantics, making [SourceIPRotator.Next]
// and [SourceIPRotator.NextForFamily] completely lock-free under parallel execution.
type SourceIPRotator struct {
	ips atomic.Pointer[[]net.IP]
	idx atomic.Uint64
	mu  sync.Mutex // serializes UpdatePool
}

// NewSourceIPRotator instantiates a [SourceIPRotator] with parsed local network addresses.
func NewSourceIPRotator(addrs []string) (*SourceIPRotator, error) {
	ips, err := parseIPs(addrs)
	if err != nil {
		return nil, err
	}

	r := &SourceIPRotator{}
	r.ips.Store(&ips)

	return r, nil
}

// Next returns the next local IP address using round-robin rotation.
// Lock-free and contention-free under parallel execution.
func (r *SourceIPRotator) Next() net.IP {
	ipsPtr := r.ips.Load()
	if ipsPtr == nil {
		return nil
	}

	ips := *ipsPtr

	n := uint64(len(ips))
	if n == 0 {
		return nil
	}

	i := r.idx.Add(1) - 1

	return ips[i%n]
}

// NextForFamily selects the next local IP matching the specified address family (IPv4 if isIPv4 is true, IPv6 otherwise).
// Lock-free and contention-free under parallel execution.
func (r *SourceIPRotator) NextForFamily(isIPv4 bool) net.IP {
	ipsPtr := r.ips.Load()
	if ipsPtr == nil {
		return nil
	}

	ips := *ipsPtr

	n := uint64(len(ips))
	if n == 0 {
		return nil
	}

	for range n {
		i := r.idx.Add(1) - 1
		ip := ips[i%n]

		hasV4 := ip.To4() != nil
		if isIPv4 == hasV4 {
			return ip
		}
	}

	return nil
}

// UpdatePool dynamically replaces active IP pool addresses and resets rotation state to zero.
func (r *SourceIPRotator) UpdatePool(addrs []string) error {
	ips, err := parseIPs(addrs)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.ips.Store(&ips)
	r.idx.Store(0)

	return nil
}

// Size returns the count of IP addresses currently registered in the pool.
func (r *SourceIPRotator) Size() int {
	ipsPtr := r.ips.Load()
	if ipsPtr == nil {
		return 0
	}

	return len(*ipsPtr)
}

// IPs returns a copy of all IP addresses in the current pool.
func (r *SourceIPRotator) IPs() []net.IP {
	ipsPtr := r.ips.Load()
	if ipsPtr == nil {
		return nil
	}

	ips := *ipsPtr
	copied := make([]net.IP, len(ips))
	copy(copied, ips)

	return copied
}

func parseIPs(addrs []string) ([]net.IP, error) {
	if len(addrs) == 0 {
		return nil, ErrEmptyIPPool
	}

	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			return nil, fmt.Errorf("aoni: invalid source IP %q", a)
		}

		ips = append(ips, ip)
	}

	return ips, nil
}

// IPv6SubnetRotator generates cryptographically random IPv6 addresses from a CIDR subnet prefix.
type IPv6SubnetRotator struct {
	prefix netip.Prefix
}

// NewIPv6SubnetRotator instantiates an [IPv6SubnetRotator] for the target CIDR prefix.
func NewIPv6SubnetRotator(cidr string) (*IPv6SubnetRotator, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is6() {
		return nil, ErrInvalidCIDR
	}

	return &IPv6SubnetRotator{prefix: prefix}, nil
}

// Next generates a new cryptographically random IPv6 address inside the configured prefix range.
func (r *IPv6SubnetRotator) Next() net.IP {
	bits := r.prefix.Bits()
	addrBytes := r.prefix.Addr().As16()

	var randomBytes [16]byte

	_, _ = rand.Read(randomBytes[:])

	byteIdx := bits / 8
	bitRem := bits % 8

	if bitRem > 0 && byteIdx < 16 {
		mask := byte(0xFF << (8 - bitRem))
		addrBytes[byteIdx] = (addrBytes[byteIdx] & mask) | (randomBytes[byteIdx] & ^mask)
		byteIdx++
	}

	for i := byteIdx; i < 16; i++ {
		addrBytes[i] = randomBytes[i]
	}

	res := make(net.IP, 16)
	copy(res, addrBytes[:])

	return res
}
