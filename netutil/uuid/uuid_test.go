// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package uuid_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/uuid"
)

func TestUUID_ValidationAndParsing_RFC9562(t *testing.T) {
	t.Parallel()

	// Nil UUID (RFC 9562 §5.9)
	assert.True(t, uuid.IsValid(uuid.Nil.String()))
	assert.True(t, uuid.Nil.IsNil())
	assert.False(t, uuid.Nil.IsMax())
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", uuid.Nil.String())

	// Max UUID (RFC 9562 §5.10)
	assert.True(t, uuid.IsValid(uuid.Max.String()))
	assert.True(t, uuid.Max.IsMax())
	assert.False(t, uuid.Max.IsNil())
	assert.Equal(t, "ffffffff-ffff-ffff-ffff-ffffffffffff", uuid.Max.String())

	// Predefined IANA Namespaces (RFC 9562 §6.6 / Table 3)
	namespaces := map[string]uuid.UUID{
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8": uuid.NamespaceDNS,
		"6ba7b811-9dad-11d1-80b4-00c04fd430c8": uuid.NamespaceURL,
		"6ba7b812-9dad-11d1-80b4-00c04fd430c8": uuid.NamespaceOID,
		"6ba7b814-9dad-11d1-80b4-00c04fd430c8": uuid.NamespaceX500,
	}
	for str, ns := range namespaces {
		assert.True(t, uuid.IsValid(str))
		assert.Equal(t, str, ns.String())
		parsed, err := uuid.Parse(str)
		require.NoError(t, err)
		assert.Equal(t, ns, parsed)
	}

	// MustParse
	assert.Equal(t, uuid.NamespaceDNS, uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"))
	assert.Panics(t, func() {
		uuid.MustParse("invalid-uuid")
	})

	// Case-insensitivity in parsing
	upperUUID := strings.ToUpper(uuid.NamespaceDNS.String())
	parsedUpper, errUpper := uuid.Parse(upperUUID)
	require.NoError(t, errUpper)
	assert.Equal(t, uuid.NamespaceDNS, parsedUpper)

	// Invalid UUID strings
	assert.False(t, uuid.IsValid("not-a-valid-uuid"))
	assert.False(t, uuid.IsValid("6ba7b810-9dad-11d1-80b4-00c04fd430c"))   // too short
	assert.False(t, uuid.IsValid("6ba7b810-9dad-11d1-80b4-00c04fd430c89")) // too long
	assert.False(t, uuid.IsValid("6ba7b810_9dad_11d1_80b4_00c04fd430c8"))  // wrong delimiters
	assert.False(t, uuid.IsValid("6ba7b810-9dad-11d1-80b4-00c04fd430cg"))  // non-hex character

	_, errShort := uuid.Parse("short")
	assert.ErrorIs(t, errShort, uuid.ErrInvalidLength)

	_, errFormat := uuid.Parse("6ba7b810_9dad_11d1_80b4_00c04fd430c8")
	assert.ErrorIs(t, errFormat, uuid.ErrInvalidFormat)
}

func TestUUIDv4_Generation_RFC9562(t *testing.T) {
	t.Parallel()

	u, err := uuid.NewV4()
	require.NoError(t, err)

	// Version 4 (RFC 9562 §4.2 & §5.4)
	assert.Equal(t, 4, u.Version())

	// Variant 10xx (RFC 9562 §4.1)
	assert.Equal(t, 2, u.Variant())

	uMust := uuid.MustNewV4()
	assert.Equal(t, 4, uMust.Version())
}

func TestUUIDv7_Generation_RFC9562(t *testing.T) {
	t.Parallel()

	// Appendix A.6 Test Vector: Tuesday, February 22, 2022 2:22:22.00 PM GMT-05:00
	// Unix Milliseconds = 1645557742000 (0x017F22E279B0)
	fixedTime := time.UnixMilli(1645557742000).UTC()
	uFixed, errFixed := uuid.NewV7(fixedTime)
	require.NoError(t, errFixed)

	assert.Equal(t, 7, uFixed.Version())
	assert.Equal(t, 2, uFixed.Variant())
	assert.Equal(t, fixedTime, uFixed.Time())

	strFixed := uFixed.String()
	assert.True(t, strings.HasPrefix(strFixed, "017f22e2-79b0-7"))

	// Monotonic time-ordering verification
	t1 := time.Now().UTC()

	time.Sleep(2 * time.Millisecond)

	t2 := time.Now().UTC()

	u1, err1 := uuid.NewV7(t1)
	require.NoError(t, err1)

	u2, err2 := uuid.NewV7(t2)
	require.NoError(t, err2)

	// Big-endian comparison (RFC 9562 §4 & §6.11)
	assert.True(t, bytes.Compare(u1[:], u2[:]) < 0)
	assert.True(t, u1.String() < u2.String())
	assert.Equal(t, t1.Truncate(time.Millisecond), u1.Time())

	// Append 0-alloc verification
	var buf [36]byte

	appended := u1.Append(buf[:0])
	assert.Equal(t, u1.String(), string(appended))

	// MustNewV7
	uMust := uuid.MustNewV7()
	assert.Equal(t, 7, uMust.Version())
}

func TestUUID_JSON_Marshaling(t *testing.T) {
	t.Parallel()

	type payload struct {
		ID uuid.UUID `json:"id"`
	}

	u := uuid.MustNewV7()
	p := payload{ID: u}

	data, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Equal(t, `{"id":"`+u.String()+`"}`, string(data))

	var unmarshaled payload

	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)
	assert.Equal(t, u, unmarshaled.ID)

	// Binary Marshaling
	bin, err := u.MarshalBinary()
	require.NoError(t, err)
	assert.Equal(t, u[:], bin)

	var uBin uuid.UUID

	err = uBin.UnmarshalBinary(bin)
	require.NoError(t, err)
	assert.Equal(t, u, uBin)
}
