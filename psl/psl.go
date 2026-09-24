// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package psl

import (
	"bytes"
	"errors"
	"fmt"
	"net/http/cookiejar"
	"net/netip"
	"strings"
)

var (
	// ErrEmptyLabel is returned when a domain contains empty labels (leading/trailing/double dots).
	ErrEmptyLabel = errors.New("aoni/psl: empty label in domain")

	// ErrCannotDeriveETLD is returned when an eTLD+1 cannot be derived for the domain.
	ErrCannotDeriveETLD = errors.New("aoni/psl: cannot derive eTLD+1 for domain")

	// ErrInvalidSuffix is returned when the derived public suffix is invalid for the domain.
	ErrInvalidSuffix = errors.New("aoni/psl: invalid public suffix")
)

// List implements the [cookiejar.PublicSuffixList] interface using Mozilla's compiled PSL dataset.
var List cookiejar.PublicSuffixList = list{}

type list struct{}

func (list) PublicSuffix(domain string) string {
	ps, _ := PublicSuffix(domain)
	return ps
}

func (list) String() string {
	return version
}

// PublicSuffix returns the public suffix of the domain using Mozilla's database compiled into the binary.
func PublicSuffix(domain string) (publicSuffix string, icann bool) {
	if _, err := netip.ParseAddr(domain); err == nil {
		return domain, false
	}

	lo, hi := uint32(0), uint32(numTLD)
	s, suffix, icannNode, wildcard := domain, len(domain), false, false

loop:
	for {
		dot := strings.LastIndexByte(s, '.')
		if wildcard {
			icann = icannNode
			suffix = 1 + dot
		}

		if lo == hi {
			break
		}

		f := find(s[1+dot:], lo, hi)
		if f == notFound {
			break
		}

		u := uint32(nodes.get(f) >> (nodesBitsTextOffset + nodesBitsTextLength))
		icannNode = u&(1<<nodesBitsICANN-1) != 0
		u >>= nodesBitsICANN
		u = children.get(u & (1<<nodesBitsChildren - 1))
		lo = u & (1<<childrenBitsLo - 1)
		u >>= childrenBitsLo
		hi = u & (1<<childrenBitsHi - 1)
		u >>= childrenBitsHi

		switch u & (1<<childrenBitsNodeType - 1) {
		case nodeTypeNormal:
			suffix = 1 + dot
		case nodeTypeException:
			suffix = 1 + len(s)
			break loop
		}

		u >>= childrenBitsNodeType

		wildcard = u&(1<<childrenBitsWildcard-1) != 0
		if !wildcard {
			icann = icannNode
		}

		if dot == -1 {
			break
		}

		s = s[:dot]
	}

	if suffix == len(domain) {
		return domain[1+strings.LastIndexByte(domain, '.'):], icann
	}

	return domain[suffix:], icann
}

func isLikelyIP(b []byte) bool {
	if len(b) == 0 {
		return false
	}

	c := b[0]

	return (c >= '0' && c <= '9') || c == ':'
}

// PublicSuffixBytes returns the public suffix slice of domain bytes without heap allocations.
func PublicSuffixBytes(domain []byte) (publicSuffix []byte, icann bool) {
	if isLikelyIP(domain) {
		if _, err := netip.ParseAddr(string(domain)); err == nil {
			return domain, false
		}
	}

	lo, hi := uint32(0), uint32(numTLD)
	s, suffix, icannNode, wildcard := domain, len(domain), false, false

loop:
	for {
		dot := bytes.LastIndexByte(s, '.')
		if wildcard {
			icann = icannNode
			suffix = 1 + dot
		}

		if lo == hi {
			break
		}

		f := findBytes(s[1+dot:], lo, hi)
		if f == notFound {
			break
		}

		u := uint32(nodes.get(f) >> (nodesBitsTextOffset + nodesBitsTextLength))
		icannNode = u&(1<<nodesBitsICANN-1) != 0
		u >>= nodesBitsICANN
		u = children.get(u & (1<<nodesBitsChildren - 1))
		lo = u & (1<<childrenBitsLo - 1)
		u >>= childrenBitsLo
		hi = u & (1<<childrenBitsHi - 1)
		u >>= childrenBitsHi

		switch u & (1<<childrenBitsNodeType - 1) {
		case nodeTypeNormal:
			suffix = 1 + dot
		case nodeTypeException:
			suffix = 1 + len(s)
			break loop
		}

		u >>= childrenBitsNodeType

		wildcard = u&(1<<childrenBitsWildcard-1) != 0
		if !wildcard {
			icann = icannNode
		}

		if dot == -1 {
			break
		}

		s = s[:dot]
	}

	if suffix == len(domain) {
		return domain[1+bytes.LastIndexByte(domain, '.'):], icann
	}

	return domain[suffix:], icann
}

// EffectiveTLDPlusOne returns the effective top level domain plus one more label.
// For example, the eTLD+1 for "foo.bar.golang.org" is "golang.org".
func EffectiveTLDPlusOne(domain string) (string, error) {
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", fmt.Errorf("%w: %q", ErrEmptyLabel, domain)
	}

	suffix, _ := PublicSuffix(domain)
	if len(domain) <= len(suffix) {
		return "", fmt.Errorf("%w: %q", ErrCannotDeriveETLD, domain)
	}

	i := len(domain) - len(suffix) - 1
	if domain[i] != '.' {
		return "", fmt.Errorf("%w: suffix %q for domain %q", ErrInvalidSuffix, suffix, domain)
	}

	return domain[1+strings.LastIndexByte(domain[:i], '.'):], nil
}

// EffectiveTLDPlusOneBytes returns the effective top level domain plus one more label
// as a subslice of domain bytes with zero heap allocations.
func EffectiveTLDPlusOneBytes(domain []byte) ([]byte, error) {
	if bytes.HasPrefix(domain, []byte(".")) || bytes.HasSuffix(domain, []byte(".")) ||
		bytes.Contains(domain, []byte("..")) {
		return nil, ErrEmptyLabel
	}

	suffix, _ := PublicSuffixBytes(domain)
	if len(domain) <= len(suffix) {
		return nil, ErrCannotDeriveETLD
	}

	i := len(domain) - len(suffix) - 1
	if domain[i] != '.' {
		return nil, ErrInvalidSuffix
	}

	return domain[1+bytes.LastIndexByte(domain[:i], '.'):], nil
}

const notFound uint32 = 1<<32 - 1

func find(label string, lo, hi uint32) uint32 {
	for lo < hi {
		mid := lo + (hi-lo)/2
		s := nodeLabel(mid)

		switch {
		case s < label:
			lo = mid + 1
		case s == label:
			return mid
		default:
			hi = mid
		}
	}

	return notFound
}

func findBytes(label []byte, lo, hi uint32) uint32 {
	for lo < hi {
		mid := lo + (hi-lo)/2
		s := nodeLabel(mid)

		cmp := compareStringBytes(s, label)
		switch {
		case cmp < 0:
			lo = mid + 1
		case cmp == 0:
			return mid
		default:
			hi = mid
		}
	}

	return notFound
}

func compareStringBytes(s string, b []byte) int {
	n := min(len(s), len(b))

	for i := range n {
		if s[i] != b[i] {
			return int(s[i]) - int(b[i])
		}
	}

	return len(s) - len(b)
}

func nodeLabel(i uint32) string {
	x := nodes.get(i)
	length := x & (1<<nodesBitsTextLength - 1)
	x >>= nodesBitsTextLength
	offset := x & (1<<nodesBitsTextOffset - 1)

	return text[offset : offset+length]
}
