// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

// PublicSuffixList provides the public suffix of a domain (RFC 6265 §5.3 step 5).
//
// The method set intentionally mirrors [net/http/cookiejar.PublicSuffixList], so any
// implementation of that interface (for example the Mozilla PSL dataset shipped by
// github.com/lemon4ksan/aoni-browser/psl) can be injected without adapters.
//
// The core module does not bundle a suffix dataset: it changes continuously and does not
// belong to the frozen protocol core. When no list is configured, cookies with an explicit
// Domain attribute are validated only by [DomainMatch].
type PublicSuffixList interface {
	// PublicSuffix returns the public suffix of domain.
	PublicSuffix(domain string) string

	// String returns a description of the list source (e.g. dataset revision).
	String() string
}

// publicSuffixAware is implemented by jars that accept an injected [PublicSuffixList].
type publicSuffixAware interface {
	SetPublicSuffixList(list PublicSuffixList)
}

// ApplyPublicSuffixList injects list into jar if the jar supports it.
// It reports whether the jar accepted the list.
func ApplyPublicSuffixList(jar any, list PublicSuffixList) bool {
	if aware, ok := jar.(publicSuffixAware); ok {
		aware.SetPublicSuffixList(list)

		return true
	}

	return false
}

// MemoryJarOption configures a [MemoryJar] at construction time.
type MemoryJarOption func(*MemoryJar)

// WithPublicSuffixList configures the [PublicSuffixList] used to reject cookies whose Domain
// attribute is a public suffix (RFC 6265 §5.3 step 5).
func WithPublicSuffixList(list PublicSuffixList) MemoryJarOption {
	return func(mj *MemoryJar) {
		mj.psl.Set(list)
	}
}
