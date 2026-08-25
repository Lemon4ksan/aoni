// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"errors"
	"net/netip"
	"sync"
)

var (
	// ErrIPPoolExhausted is returned when no available IP addresses remain in the configured pool.
	ErrIPPoolExhausted = errors.New("aoni/masque: ip address pool exhausted")

	// ErrAddressAlreadyAllocated is returned when the requested static IP is already assigned.
	ErrAddressAlreadyAllocated = errors.New("aoni/masque: requested address already allocated")
)

// IPAM manages client IP allocation and leasing for MASQUE connect-ip tunnels (RFC 9484 §4.7.1 & §4.7.2).
type IPAM struct {
	mu         sync.Mutex
	ipv4Prefix netip.Prefix
	ipv6Prefix netip.Prefix
	allocated  map[netip.Addr]bool
	nextIPv4   uint32
	nextIPv6   uint64
}

// NewIPAM constructs an [IPAM] allocator managing the given IPv4 and IPv6 subnets.
func NewIPAM(v4Prefix, v6Prefix netip.Prefix) *IPAM {
	if !v4Prefix.IsValid() {
		v4Prefix = netip.MustParsePrefix("10.8.0.0/24")
	}

	if !v6Prefix.IsValid() {
		v6Prefix = netip.MustParsePrefix("fd00:a011::/64")
	}

	return &IPAM{
		ipv4Prefix: v4Prefix,
		ipv6Prefix: v6Prefix,
		allocated:  make(map[netip.Addr]bool),
		nextIPv4:   2, // Start at .2 (.1 reserved for gateway)
		nextIPv6:   2,
	}
}

// Allocate leases an IP address for the client. If specific requests match the subnet and are free,
// they are honored; otherwise, the next free address in the subnet is leased.
func (m *IPAM) Allocate(reqs []RequestedAddress) (AssignedAddress, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if any requested address is valid, in-pool, and available
	for _, req := range reqs {
		if req.Addr.IsValid() && (m.ipv4Prefix.Contains(req.Addr) || m.ipv6Prefix.Contains(req.Addr)) {
			if !m.allocated[req.Addr] {
				m.allocated[req.Addr] = true

				var pLen byte = 32
				if req.Addr.Is6() {
					pLen = 128
				}

				return AssignedAddress{
					Addr:         req.Addr,
					RequestID:    req.RequestID,
					IPVersion:    req.IPVersion,
					PrefixLength: pLen,
				}, nil
			}
		}
	}

	// Default: Allocate next IPv4 address from pool
	base4 := m.ipv4Prefix.Addr().As4()
	baseInt := (uint32(base4[0]) << 24) | (uint32(base4[1]) << 16) | (uint32(base4[2]) << 8) | uint32(base4[3])
	mask := ^uint32(0) << (32 - m.ipv4Prefix.Bits())
	maxHosts := ^mask

	for range maxHosts {
		currHost := m.nextIPv4
		m.nextIPv4++

		if m.nextIPv4 >= maxHosts {
			m.nextIPv4 = 2
		}

		ipInt := (baseInt & mask) | currHost
		candidate := netip.AddrFrom4([4]byte{
			byte(ipInt >> 24),
			byte(ipInt >> 16),
			byte(ipInt >> 8),
			byte(ipInt),
		})

		if !m.allocated[candidate] {
			m.allocated[candidate] = true

			var reqID uint64
			if len(reqs) > 0 {
				reqID = reqs[0].RequestID
			}

			return AssignedAddress{
				Addr:         candidate,
				RequestID:    reqID,
				IPVersion:    4,
				PrefixLength: 32,
			}, nil
		}
	}

	return AssignedAddress{}, ErrIPPoolExhausted
}

// Release returns an allocated IP address back to the pool.
func (m *IPAM) Release(addr netip.Addr) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.allocated, addr)
}

// IsAllocated reports whether addr is currently leased.
func (m *IPAM) IsAllocated(addr netip.Addr) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.allocated[addr]
}
