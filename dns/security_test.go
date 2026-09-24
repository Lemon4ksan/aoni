// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/dns"
	"github.com/lemon4ksan/mach/proto/dns/wire"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
)

func TestSecurity_GenerateQueryID_RFC5452(t *testing.T) {
	t.Parallel()

	// Ensure consecutive IDs vary (cryptographically random)
	id1 := dns.GenerateQueryID()
	id2 := dns.GenerateQueryID()
	assert.NotEqual(t, id1, id2)
}

func TestSecurity_ValidateQueryMatch_RFC5452(t *testing.T) {
	t.Parallel()

	const (
		id    uint16 = 0x1234
		qname        = "example.com."
		qtype        = wire.TypeA
		qclas        = wire.ClassIN
	)

	// Valid Match (case-insensitive and root dot agnostic)
	err := dns.ValidateQueryMatch(id, id, qname, "EXAMPLE.COM", qtype, qtype, qclas, qclas)
	require.NoError(t, err)

	// Mismatched ID
	errID := dns.ValidateQueryMatch(id, 0x9999, qname, qname, qtype, qtype, qclas, qclas)
	assert.ErrorIs(t, errID, dns.ErrSpoofedID)

	// Mismatched QName
	errName := dns.ValidateQueryMatch(id, id, qname, "other.org", qtype, qtype, qclas, qclas)
	assert.ErrorIs(t, errName, dns.ErrSpoofedQName)

	// Mismatched QType
	errType := dns.ValidateQueryMatch(id, id, qname, qname, qtype, wire.TypeAAAA, qclas, qclas)
	assert.ErrorIs(t, errType, dns.ErrSpoofedQType)

	// Mismatched QClass
	errClass := dns.ValidateQueryMatch(id, id, qname, qname, qtype, qtype, qclas, wire.ClassCH)
	assert.ErrorIs(t, errClass, dns.ErrSpoofedQClass)
}
