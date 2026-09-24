// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"

	"github.com/lemon4ksan/aoni/dns"
)

func TestIDNA_IsLDH(t *testing.T) {
	t.Parallel()

	assert.True(t, dns.IsLDH("example"))
	assert.True(t, dns.IsLDH("sub-domain-123"))
	assert.True(t, dns.IsLDH("A1"))

	assert.False(t, dns.IsLDH("-leading"))
	assert.False(t, dns.IsLDH("trailing-"))
	assert.False(t, dns.IsLDH("_tcp"))
	assert.False(t, dns.IsLDH(""))
	assert.False(t, dns.IsLDH(string(make([]byte, 64))))
}

func TestIDNA_ClassifyLabel(t *testing.T) {
	t.Parallel()

	// NR-LDH (RFC 5890 §2.3.2.2)
	assert.Equal(t, dns.LabelTypeNRLDH, dns.ClassifyLabel("example"))
	assert.Equal(t, "NR-LDH", dns.LabelTypeNRLDH.String())

	// A-label (RFC 5890 §2.3.2.1)
	assert.Equal(t, dns.LabelTypeALabel, dns.ClassifyLabel("xn--e1afmkfd"))
	assert.Equal(t, "A-label", dns.LabelTypeALabel.String())

	// U-label (RFC 5890 §2.3.2.1)
	assert.Equal(t, dns.LabelTypeULabel, dns.ClassifyLabel("пример"))
	assert.Equal(t, "U-label", dns.LabelTypeULabel.String())

	// R-LDH (RFC 5890 §2.3.1)
	assert.Equal(t, dns.LabelTypeRLDH, dns.ClassifyLabel("ab--reserved"))
	assert.Equal(t, "R-LDH", dns.LabelTypeRLDH.String())

	// Fake A-label (RFC 5890 §2.3.1)
	assert.Equal(t, dns.LabelTypeFakeALabel, dns.ClassifyLabel("xn--0"))
	assert.Equal(t, "Fake A-label", dns.LabelTypeFakeALabel.String())

	// Non-LDH (RFC 5890 §2.3.1)
	assert.Equal(t, dns.LabelTypeNonLDH, dns.ClassifyLabel("_tcp"))
	assert.Equal(t, "Non-LDH", dns.LabelTypeNonLDH.String())
}

func TestIDNA_IsIDN(t *testing.T) {
	t.Parallel()

	assert.True(t, dns.IsIDN("президент.рф"))
	assert.True(t, dns.IsIDN("xn--d1abbgf6aiiy.xn--p1ai"))
	assert.True(t, dns.IsIDN("sub.xn--e1afmkfd.com"))

	assert.False(t, dns.IsIDN("example.com"))
	assert.False(t, dns.IsIDN("sub.api.example.com."))
	assert.False(t, dns.IsIDN(""))
}

func TestIDNA_EqualFoldASCII(t *testing.T) {
	t.Parallel()

	assert.True(t, dns.EqualFoldASCII("foo.example.net.", "Foo.ExamplE.net"))
	assert.True(t, dns.EqualFoldASCII("aol.com", "AOL.COM."))
	assert.False(t, dns.EqualFoldASCII("example.com", "example.org"))

	// Non-ASCII binary octets
	assert.False(t, dns.EqualFoldASCII("\xdd.example.com", "\xfd.example.com"))
	assert.True(t, dns.EqualFoldASCII("\xdd.example.com", "\xdd.EXAMPLE.COM"))
}

func TestIDNA_CanonicalDomainName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "foo.example.net.", dns.CanonicalDomainName("Foo.ExamplE.net."))
	assert.Equal(t, "aol.com", dns.CanonicalDomainName("AOL.COM"))
}
