// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package psl_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/psl"
)

var testCases = []struct {
	domain    string
	wantPS    string
	wantICANN bool
}{
	{"", "", false},
	{"192.0.2.0", "192.0.2.0", false},
	{"2001:db8::", "2001:db8::", false},
	{"ao", "ao", true},
	{"www.ao", "ao", true},
	{"pb.ao", "pb.ao", true},
	{"www.pb.ao", "pb.ao", true},
	{"com.ar", "com.ar", true},
	{"www.com.ar", "com.ar", true},
	{"google.com", "com", true},
	{"maps.google.com", "com", true},
	{"amazon.co.uk", "co.uk", true},
	{"foo.dyndns.org", "dyndns.org", false},
	{"foo.intranet", "intranet", false},
	{"city.kobe.jp", "kobe.jp", true},
}

func TestPublicSuffix(t *testing.T) {
	t.Parallel()

	for _, tc := range testCases {
		gotPS, gotICANN := psl.PublicSuffix(tc.domain)
		assert.Equalf(t, tc.wantPS, gotPS, "domain: %s", tc.domain)
		assert.Equalf(t, tc.wantICANN, gotICANN, "domain: %s", tc.domain)
	}
}

func TestPublicSuffixBytes(t *testing.T) {
	t.Parallel()

	for _, tc := range testCases {
		gotPS, gotICANN := psl.PublicSuffixBytes([]byte(tc.domain))
		assert.Equalf(t, tc.wantPS, string(gotPS), "domain: %s", tc.domain)
		assert.Equalf(t, tc.wantICANN, gotICANN, "domain: %s", tc.domain)
	}
}

func TestEffectiveTLDPlusOne(t *testing.T) {
	t.Parallel()

	etld, err := psl.EffectiveTLDPlusOne("foo.bar.golang.org")
	require.NoError(t, err)
	assert.Equal(t, "golang.org", etld)

	etld, err = psl.EffectiveTLDPlusOne("www.books.amazon.co.uk")
	require.NoError(t, err)
	assert.Equal(t, "amazon.co.uk", etld)

	_, err = psl.EffectiveTLDPlusOne("com")
	require.Error(t, err)

	_, err = psl.EffectiveTLDPlusOne(".golang.org")
	require.Error(t, err)
}

func TestEffectiveTLDPlusOneBytes(t *testing.T) {
	t.Parallel()

	etld, err := psl.EffectiveTLDPlusOneBytes([]byte("foo.bar.golang.org"))
	require.NoError(t, err)
	assert.Equal(t, "golang.org", string(etld))

	etld, err = psl.EffectiveTLDPlusOneBytes([]byte("www.books.amazon.co.uk"))
	require.NoError(t, err)
	assert.Equal(t, "amazon.co.uk", string(etld))

	_, err = psl.EffectiveTLDPlusOneBytes([]byte("com"))
	require.Error(t, err)
}

func TestCookieJarCompliance(t *testing.T) {
	t.Parallel()

	jarPSL := psl.List
	assert.Equal(t, "co.uk", jarPSL.PublicSuffix("amazon.co.uk"))
	assert.NotEmpty(t, jarPSL.String())
}
