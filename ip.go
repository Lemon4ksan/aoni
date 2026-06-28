// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

// SourceIPRotator is a struct that holds a pool of source IP addresses and rotates between them.
type SourceIPRotator struct {
	ips []net.IP
	mu  sync.Mutex
	idx int
}

// NewSourceIPRotator creates a new SourceIPRotator with the given IP addresses.
func NewSourceIPRotator(addrs []string) (*SourceIPRotator, error) {
	var ips []net.IP
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			return nil, fmt.Errorf("aoni: invalid source IP %q", a)
		}

		ips = append(ips, ip)
	}

	if len(ips) == 0 {
		return nil, errors.New("aoni: source IP pool cannot be empty")
	}

	return &SourceIPRotator{ips: ips}, nil
}

// Next returns the next IP address in the pool and rotates to the next one.
func (r *SourceIPRotator) Next() net.IP {
	r.mu.Lock()
	defer r.mu.Unlock()

	ip := r.ips[r.idx]
	r.idx = (r.idx + 1) % len(r.ips)

	return ip
}

// NextForFamily returns the next IP address in the pool that matches the requested IP family
// (IPv4 if isIPv4 is true, IPv6 otherwise).
// Returns nil if no matching IP address family is found in the current pool.
func (r *SourceIPRotator) NextForFamily(isIPv4 bool) net.IP {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(r.ips)
	for range n {
		ip := r.ips[r.idx]
		r.idx = (r.idx + 1) % n

		hasV4 := ip.To4() != nil
		if isIPv4 == hasV4 {
			return ip
		}
	}

	return nil
}

// UpdatePool dynamically replaces the IP addresses in the pool on the fly.
// Resets the rotation index to 0. Returns an error if the new list is empty or invalid.
func (r *SourceIPRotator) UpdatePool(addrs []string) error {
	var ips []net.IP
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			return fmt.Errorf("aoni: invalid source IP %q", a)
		}

		ips = append(ips, ip)
	}

	if len(ips) == 0 {
		return errors.New("aoni: source IP pool cannot be empty")
	}

	r.mu.Lock()
	r.ips = ips
	r.idx = 0
	r.mu.Unlock()

	return nil
}

// Size returns the total number of IP addresses currently registered in the pool.
func (r *SourceIPRotator) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ips)
}

// IPs returns a safe copy of the current IP pool addresses.
func (r *SourceIPRotator) IPs() []net.IP {
	r.mu.Lock()
	defer r.mu.Unlock()

	copied := make([]net.IP, len(r.ips))
	copy(copied, r.ips)

	return copied
}
