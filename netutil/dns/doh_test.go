// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/lemon4ksan/foundation/net/dns/wire"
	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/netutil/dns"
	"github.com/lemon4ksan/aoni/netutil/svcb"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewDoHResolver(t *testing.T) {
	t.Parallel()

	resolver := dns.NewDoHResolver("https://8.8.8.8/dns-query", "dns.google", nil)
	require.NotNil(t, resolver)

	assert.Equal(t, "https://8.8.8.8/dns-query", resolver.Endpoint)
	assert.Equal(t, "dns.google", resolver.Host)
	assert.Equal(t, dns.DoHMethodPost, resolver.Method)
}

func TestDoHResolver_LookupIPAddr_Mocked(t *testing.T) {
	t.Parallel()

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "https://1.1.1.1/dns-query", req.URL.String())
			assert.Equal(t, "cloudflare-dns.com", req.Host)
			assert.Equal(t, dns.DoHMediaType, req.Header.Get("Accept"))
			assert.Equal(t, dns.DoHMediaType, req.Header.Get("Content-Type"))

			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}

			if len(body) < 12 {
				return nil, errors.New("query too short")
			}

			txID := binary.BigEndian.Uint16(body[0:2])
			isAAAA := bytes.Contains(body, []byte{0x00, 0x1c})

			var (
				resp bytes.Buffer
				h    [12]byte
			)

			binary.BigEndian.PutUint16(h[0:2], txID)
			binary.BigEndian.PutUint16(h[2:4], 0x8180)
			binary.BigEndian.PutUint16(h[4:6], 1) // QDCOUNT = 1
			binary.BigEndian.PutUint16(h[6:8], 1) // ANCOUNT = 1
			resp.Write(h[:])

			// Question: example.com
			resp.WriteByte(7)
			resp.WriteString("example")
			resp.WriteByte(3)
			resp.WriteString("com")
			resp.WriteByte(0)

			var qtail [4]byte

			qtype := wire.TypeA
			if isAAAA {
				qtype = wire.TypeAAAA
			}

			binary.BigEndian.PutUint16(qtail[0:2], qtype)
			binary.BigEndian.PutUint16(qtail[2:4], wire.ClassIN)
			resp.Write(qtail[:])

			// Answer: compression pointer 0xc00c
			var anHeader [10]byte
			binary.BigEndian.PutUint16(anHeader[0:2], qtype)
			binary.BigEndian.PutUint16(anHeader[2:4], wire.ClassIN)
			binary.BigEndian.PutUint32(anHeader[4:8], 300)

			if isAAAA {
				binary.BigEndian.PutUint16(anHeader[8:10], 16)
				resp.Write([]byte{0xc0, 0x0c})
				resp.Write(anHeader[:])
				resp.Write(net.ParseIP("2606:4700:4700::1111").To16())
			} else {
				binary.BigEndian.PutUint16(anHeader[8:10], 4)
				resp.Write([]byte{0xc0, 0x0c})
				resp.Write(anHeader[:])
				resp.Write(net.ParseIP("1.1.1.1").To4())
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(resp.Bytes())),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resolver := dns.NewDoHResolver("https://1.1.1.1/dns-query", "cloudflare-dns.com", aoni.NewClient(mockClient))

	// Test LookupIPAddr
	ips, err := resolver.LookupIPAddr(t.Context(), "example.com")
	require.NoError(t, err)
	require.Len(t, ips, 2)
	assert.Equal(t, "1.1.1.1", ips[0].IP.String())
	assert.Equal(t, "2606:4700:4700::1111", ips[1].IP.String())

	// Test LookupNetIP
	netIPs, errNet := resolver.LookupNetIP(t.Context(), "example.com")
	require.NoError(t, errNet)
	require.Len(t, netIPs, 2)
	assert.Equal(t, netip.MustParseAddr("1.1.1.1"), netIPs[0])
	assert.Equal(t, netip.MustParseAddr("2606:4700:4700::1111"), netIPs[1])

	// Test LookupDNSRecords
	records, errRec := resolver.LookupDNSRecords(t.Context(), "example.com")
	require.NoError(t, errRec)
	require.Len(t, records, 2)
	assert.Equal(t, uint32(300), records[0].TTL)
}

func TestDoHResolver_LookupIPAddr_QueryFailure(t *testing.T) {
	t.Parallel()

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("server error")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resolver := dns.NewDoHResolver("https://1.1.1.1/dns-query", "cloudflare-dns.com", aoni.NewClient(mockClient))

	_, err := resolver.LookupIPAddr(t.Context(), "example.com")
	assert.Error(t, err)
}

func TestDoHResolver_QueryEncoding(t *testing.T) {
	t.Parallel()

	var (
		capturedContentType string
		capturedBody        []byte
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", dns.DoHMediaType)
		w.WriteHeader(http.StatusOK)

		if len(capturedBody) >= 2 {
			txID := binary.BigEndian.Uint16(capturedBody[0:2])
			resp := make([]byte, 12)
			binary.BigEndian.PutUint16(resp[0:2], txID)
			binary.BigEndian.PutUint16(resp[2:4], 0x8180)
			_, _ = w.Write(resp)
		}
	}))
	t.Cleanup(ts.Close)

	resolver := dns.NewDoHResolver(ts.URL, "cloudflare-dns.com", aoni.NewClient(ts.Client()))

	ctx := t.Context()
	_, err := resolver.LookupWireRecord(ctx, "example.com", 1)
	require.NoError(t, err)

	assert.Equal(t, dns.DoHMediaType, capturedContentType)
	assert.NotEmpty(t, capturedBody)
}

func TestDoHResolver_EDNS0_And_GetMethod(t *testing.T) {
	t.Parallel()

	t.Run("doh_get_method_base64_encoded", func(t *testing.T) {
		t.Parallel()

		var (
			mu             sync.Mutex
			capturedMethod string
			capturedQuery  string
		)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query().Get("dns")

			mu.Lock()
			capturedMethod = r.Method
			capturedQuery = query
			mu.Unlock()

			w.Header().Set("Content-Type", dns.DoHMediaType)
			w.WriteHeader(http.StatusOK)

			decoded, err := base64.RawURLEncoding.DecodeString(query)
			if err == nil && len(decoded) >= 2 {
				txID := binary.BigEndian.Uint16(decoded[0:2])
				resp := make([]byte, 12)
				binary.BigEndian.PutUint16(resp[0:2], txID)
				binary.BigEndian.PutUint16(resp[2:4], 0x8180)
				_, _ = w.Write(resp)
			}
		}))
		t.Cleanup(ts.Close)

		resolver := dns.NewDoHResolver(ts.URL, "cloudflare-dns.com", aoni.NewClient(ts.Client()))
		resolver.Method = dns.DoHMethodGet

		_, err := resolver.LookupWireRecord(t.Context(), "example.com", wire.TypeA)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()

		assert.Equal(t, http.MethodGet, capturedMethod)
		assert.NotEmpty(t, capturedQuery)

		decoded, decErr := base64.RawURLEncoding.DecodeString(capturedQuery)
		require.NoError(t, decErr)
		assert.GreaterOrEqual(t, len(decoded), 12)
	})

	t.Run("doh_post_method_edns0_padding", func(t *testing.T) {
		t.Parallel()

		var (
			mu           sync.Mutex
			capturedBody []byte
		)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)

			mu.Lock()
			capturedBody = body
			mu.Unlock()

			w.Header().Set("Content-Type", dns.DoHMediaType)
			w.WriteHeader(http.StatusOK)

			if len(body) >= 2 {
				txID := binary.BigEndian.Uint16(body[0:2])
				resp := make([]byte, 12)
				binary.BigEndian.PutUint16(resp[0:2], txID)
				binary.BigEndian.PutUint16(resp[2:4], 0x8180)
				_, _ = w.Write(resp)
			}
		}))
		t.Cleanup(ts.Close)

		resolver := dns.NewDoHResolver(ts.URL, "cloudflare-dns.com", aoni.NewClient(ts.Client()))
		resolver.Method = dns.DoHMethodPost
		resolver.EDNS.PadToBlock = 128

		_, err := resolver.LookupWireRecord(t.Context(), "example.com", wire.TypeA)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()

		assert.NotEmpty(t, capturedBody)
		assert.Equal(t, 0, len(capturedBody)%128, "wire query should be padded to 128 bytes block size")
	})
}

func TestDoHResolver_LookupHTTPS_And_LookupSVCB(t *testing.T) {
	t.Parallel()

	httpsRec := &svcb.Record{
		Priority:   1,
		TargetName: "endpoint4.example.com",
		Params: map[svcb.SvcParamKey][]byte{
			svcb.ParamALPN: svcb.EncodeALPN([]string{"h2", "h3-29"}),
			svcb.ParamPort: svcb.EncodePort(443),
		},
	}

	rdata, err := httpsRec.MarshalRDATA()
	require.NoError(t, err)

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			isSVCB := bytes.Contains(body, []byte{0x00, byte(svcb.TypeSVCB)})

			qtype := svcb.TypeHTTPS
			if isSVCB {
				qtype = svcb.TypeSVCB
			}

			var (
				buf    bytes.Buffer
				header [12]byte
			)

			binary.BigEndian.PutUint16(header[0:2], 0x1234)
			binary.BigEndian.PutUint16(header[2:4], 0x8180)
			binary.BigEndian.PutUint16(header[4:6], 1)
			binary.BigEndian.PutUint16(header[6:8], 1)
			buf.Write(header[:])

			// Question
			buf.WriteByte(7)
			buf.WriteString("example")
			buf.WriteByte(3)
			buf.WriteString("com")
			buf.WriteByte(0)

			var qtail [4]byte
			binary.BigEndian.PutUint16(qtail[0:2], qtype)
			binary.BigEndian.PutUint16(qtail[2:4], wire.ClassIN)
			buf.Write(qtail[:])

			// Answer: compression pointer 0xc00c, qtype, ClassIN, TTL 300
			var anHeader [10]byte
			binary.BigEndian.PutUint16(anHeader[0:2], qtype)
			binary.BigEndian.PutUint16(anHeader[2:4], wire.ClassIN)
			binary.BigEndian.PutUint32(anHeader[4:8], 300)
			binary.BigEndian.PutUint16(anHeader[8:10], uint16(len(rdata)))
			buf.Write([]byte{0xc0, 0x0c})
			buf.Write(anHeader[:])
			buf.Write(rdata)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resolver := dns.NewDoHResolver("https://1.1.1.1/dns-query", "cloudflare-dns.com", aoni.NewClient(mockClient))

	records, err := resolver.LookupHTTPS(t.Context(), "example.com", 443)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, uint16(1), records[0].Priority)
	assert.Equal(t, "endpoint4.example.com", records[0].TargetName)

	svcbRecords, errSVCB := resolver.LookupSVCB(t.Context(), "http", "example.com", 443)
	require.NoError(t, errSVCB)
	require.Len(t, svcbRecords, 1)
}
