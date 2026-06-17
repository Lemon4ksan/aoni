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
