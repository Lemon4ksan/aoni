// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildMockDNSHeader(id, flags, qdCount, anCount, nsCount, arCount uint16) []byte {
	var buf [12]byte
	binary.BigEndian.PutUint16(buf[0:2], id)
	binary.BigEndian.PutUint16(buf[2:4], flags)
	binary.BigEndian.PutUint16(buf[4:6], qdCount)
	binary.BigEndian.PutUint16(buf[6:8], anCount)
	binary.BigEndian.PutUint16(buf[8:10], nsCount)
	binary.BigEndian.PutUint16(buf[10:12], arCount)

	return buf[:]
}

func TestPackDNSQuery_Errors(t *testing.T) {
	t.Parallel()

	t.Run("empty_domain_returns_invalid_domain_error", func(t *testing.T) {
		t.Parallel()

		_, err := PackDNSQuery(1, "", TypeA)
		assert.ErrorIs(t, err, ErrInvalidDomain)
	})

	t.Run("label_exceeding_63_chars_returns_invalid_domain_error", func(t *testing.T) {
		t.Parallel()

		longLabel := strings.Repeat("a", 64) + ".com"
		_, err := PackDNSQuery(1, longLabel, TypeA)
		assert.ErrorIs(t, err, ErrInvalidDomain)
	})
}

func TestPackDNSQueryExtended_PaddingAndECS(t *testing.T) {
	t.Parallel()

	t.Run("default_pack_query_applies_128_byte_padding", func(t *testing.T) {
		t.Parallel()

		query, err := PackDNSQuery(0x1234, "example.com", TypeA)
		require.NoError(t, err)
		assert.Equal(t, 0, len(query)%128, "Packet length %d should be padded to multiple of 128", len(query))
	})

	t.Run("edns0_client_subnet_ipv4", func(t *testing.T) {
		t.Parallel()

		edns := EDNSOptions{
			ClientIP:   netip.MustParseAddr("192.168.1.100"),
			PadToBlock: 128,
		}

		query, err := PackDNSQueryExtended(0x5678, "api.example.org", TypeA, edns)
		require.NoError(t, err)
		require.NotEmpty(t, query)

		arCount := binary.BigEndian.Uint16(query[10:12])
		assert.Equal(t, uint16(1), arCount)

		assert.Equal(t, 0, len(query)%128)
	})

	t.Run("edns0_client_subnet_ipv6", func(t *testing.T) {
		t.Parallel()

		edns := EDNSOptions{
			ClientIP:   netip.MustParseAddr("2001:db8::1"),
			PadToBlock: 256,
		}

		query, err := PackDNSQueryExtended(0x9abc, "ipv6.example.org", TypeAAAA, edns)
		require.NoError(t, err)

		assert.Equal(t, 0, len(query)%256)
	})
}

func TestParseDNSResponse_ValidRecords(t *testing.T) {
	t.Parallel()

	t.Run("parse_a_and_aaaa_records_with_ttl", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		buf.Write(buildMockDNSHeader(0x1234, 0x8100, 1, 2, 0, 0))

		// Question section: "example.com", TypeA, ClassIN
		buf.Write([]byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0})

		var qTail [4]byte
		binary.BigEndian.PutUint16(qTail[0:2], TypeA)
		binary.BigEndian.PutUint16(qTail[2:4], ClassIN)
		buf.Write(qTail[:])

		// Answer 1: Pointer (0xC00C), TypeA, ClassIN, TTL 300, RDLen 4, IP 1.2.3.4
		buf.Write([]byte{0xc0, 0x0c})

		var ans1 [10]byte
		binary.BigEndian.PutUint16(ans1[0:2], TypeA)
		binary.BigEndian.PutUint16(ans1[2:4], ClassIN)
		binary.BigEndian.PutUint32(ans1[4:8], 300)
		binary.BigEndian.PutUint16(ans1[8:10], 4)
		buf.Write(ans1[:])
		buf.Write([]byte{1, 2, 3, 4})

		// Answer 2: Pointer (0xC00C), TypeAAAA, ClassIN, TTL 600, RDLen 16, IP 2001:db8::1
		buf.Write([]byte{0xc0, 0x0c})

		var ans2 [10]byte
		binary.BigEndian.PutUint16(ans2[0:2], TypeAAAA)
		binary.BigEndian.PutUint16(ans2[2:4], ClassIN)
		binary.BigEndian.PutUint32(ans2[4:8], 600)
		binary.BigEndian.PutUint16(ans2[8:10], 16)
		buf.Write(ans2[:])
		buf.Write(netip.MustParseAddr("2001:db8::1").AsSlice())

		records, err := ParseDNSResponseRecords(buf.Bytes(), 0x1234)
		require.NoError(t, err)
		require.Len(t, records, 2)

		assert.Equal(t, "1.2.3.4", records[0].Addr.String())
		assert.Equal(t, uint32(300), records[0].TTL)

		assert.Equal(t, "2001:db8::1", records[1].Addr.String())
		assert.Equal(t, uint32(600), records[1].TTL)

		addrs, err := ParseDNSResponse(buf.Bytes(), 0x1234)
		require.NoError(t, err)
		require.Len(t, addrs, 2)
		assert.Equal(t, "1.2.3.4", addrs[0].String())
		assert.Equal(t, "2001:db8::1", addrs[1].String())
	})
}

func TestParseDNSResponse_Errors(t *testing.T) {
	t.Parallel()

	t.Run("truncated_message_less_than_12_bytes", func(t *testing.T) {
		t.Parallel()

		_, err := ParseDNSResponse([]byte{0x00, 0x01, 0x02}, 0x0001)
		assert.ErrorIs(t, err, ErrTruncatedDNSMessage)
	})

	t.Run("transaction_id_mismatch", func(t *testing.T) {
		t.Parallel()

		msg := buildMockDNSHeader(0x1111, 0x8100, 0, 0, 0, 0)
		_, err := ParseDNSResponse(msg, 0x2222)
		assert.ErrorIs(t, err, ErrDNSResponseCode)
	})

	t.Run("transaction_id_zero_matches_any_id", func(t *testing.T) {
		t.Parallel()

		msg := buildMockDNSHeader(0x7777, 0x8100, 0, 0, 0, 0)
		_, err := ParseDNSResponse(msg, 0)
		assert.NoError(t, err)
	})

	t.Run("non_zero_rcode_returns_error", func(t *testing.T) {
		t.Parallel()

		msg := buildMockDNSHeader(0x1234, 0x8103, 0, 0, 0, 0)
		_, err := ParseDNSResponse(msg, 0x1234)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDNSResponseCode)
		assert.Contains(t, err.Error(), "rcode=3")
	})
}

func TestSkipDomainName(t *testing.T) {
	t.Parallel()

	t.Run("valid_uncompressed_domain_name", func(t *testing.T) {
		t.Parallel()

		msg := []byte{0x03, 'f', 'o', 'o', 0x03, 'b', 'a', 'r', 0x00, 0xFF}
		nextOffset, err := SkipDomainName(msg, 0)
		require.NoError(t, err)
		assert.Equal(t, 9, nextOffset)
	})

	t.Run("compression_pointer", func(t *testing.T) {
		t.Parallel()

		msg := []byte{0xC0, 0x0C, 0xFF}
		nextOffset, err := SkipDomainName(msg, 0)
		require.NoError(t, err)
		assert.Equal(t, 2, nextOffset)
	})

	t.Run("out_of_bounds_offset", func(t *testing.T) {
		t.Parallel()

		msg := []byte{0x03, 'f', 'o', 'o'}
		_, err := SkipDomainName(msg, 0)
		assert.ErrorIs(t, err, ErrTruncatedDNSMessage)
	})
}

func TestExtractECHFromHTTPSResponse(t *testing.T) {
	t.Parallel()

	t.Run("successful_ech_extraction_from_https_record", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		buf.Write(buildMockDNSHeader(0x4321, 0x8100, 1, 1, 0, 0))

		buf.Write([]byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0})

		var qTail [4]byte
		binary.BigEndian.PutUint16(qTail[0:2], TypeHTTPS)
		binary.BigEndian.PutUint16(qTail[2:4], ClassIN)
		buf.Write(qTail[:])

		buf.Write([]byte{0xc0, 0x0c})

		echPayload := []byte("ech-raw-bytes-12345")

		var (
			rdata    bytes.Buffer
			priority [2]byte
		)

		binary.BigEndian.PutUint16(priority[:], 1)
		rdata.Write(priority[:])
		rdata.WriteByte(0x00)

		var paramHeader [4]byte
		binary.BigEndian.PutUint16(paramHeader[0:2], 5)
		binary.BigEndian.PutUint16(paramHeader[2:4], uint16(len(echPayload)))
		rdata.Write(paramHeader[:])
		rdata.Write(echPayload)

		rdataBytes := rdata.Bytes()

		var ansHdr [10]byte
		binary.BigEndian.PutUint16(ansHdr[0:2], TypeHTTPS)
		binary.BigEndian.PutUint16(ansHdr[2:4], ClassIN)
		binary.BigEndian.PutUint32(ansHdr[4:8], 300)
		binary.BigEndian.PutUint16(ansHdr[8:10], uint16(len(rdataBytes)))
		buf.Write(ansHdr[:])
		buf.Write(rdataBytes)

		extracted, err := ExtractECHFromHTTPSResponse(buf.Bytes(), 0x4321)
		require.NoError(t, err)
		assert.Equal(t, echPayload, extracted)
	})

	t.Run("returns_err_ech_config_not_found_if_no_https_record_present", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		buf.Write(buildMockDNSHeader(0x4321, 0x8100, 0, 0, 0, 0))

		_, err := ExtractECHFromHTTPSResponse(buf.Bytes(), 0x4321)
		assert.ErrorIs(t, err, ErrECHConfigNotFound)
	})

	t.Run("truncated_header_returns_err_truncated_dns_message", func(t *testing.T) {
		t.Parallel()

		_, err := ExtractECHFromHTTPSResponse([]byte{0x00, 0x01}, 0x0001)
		assert.ErrorIs(t, err, ErrTruncatedDNSMessage)
	})
}
