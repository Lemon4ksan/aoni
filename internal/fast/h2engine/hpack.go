// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"bytes"
	"fmt"

	"github.com/lemon4ksan/foundation/net/http/rodata"
	"github.com/lemon4ksan/foundation/silicon/pool"
)

const (
	// indexByte represents the 1-bit MSB pattern (1xxxxxxx) for indexed header field representations (RFC 7541 §6.1).
	indexByte = 128

	// literalByte represents the 2-bit pattern (01xxxxxx) for literal header field with incremental indexing (RFC 7541 §6.2.1).
	literalByte = 64

	// noIndexByte represents the 4-bit pattern (0000xxxx / 0001xxxx) for literal header fields without indexing or never indexed (RFC 7541 §6.2.2 & §6.2.3).
	noIndexByte = 240

	// maxIndex marks the boundary index for dynamic table entries (RFC 7541 §2.3.3 & Appendix A: 61 static entries, dynamic starts at 62).
	maxIndex = 62
)

var headerStorage = pool.NewPerPStorage(func() *HeaderField {
	return &HeaderField{}
})

// HeaderField represents an HTTP header key-value pair inside HPACK static and dynamic indexing tables (RFC 7541 §1.3 & §2.3).
type HeaderField struct {
	key      []byte
	value    []byte
	keyBuf   []byte
	valBuf   []byte
	sensible bool
	static   bool
}

// AcquireHeaderField retrieves a recycled HeaderField from memory pools.
func AcquireHeaderField() *HeaderField {
	return headerStorage.Get()
}

// ReleaseHeaderField clears and returns a HeaderField to memory pools.
func ReleaseHeaderField(hf *HeaderField) {
	if hf != nil && !hf.static {
		hf.Reset()
		headerStorage.Put(hf)
	}
}

func (hf *HeaderField) String() string { return string(hf.AppendBytes(nil)) }
func (hf *HeaderField) Empty() bool    { return len(hf.key) == 0 && len(hf.value) == 0 }

func (hf *HeaderField) Reset() {
	if hf.static {
		return
	}

	hf.key = nil
	hf.value = nil
	hf.keyBuf = hf.keyBuf[:0]
	hf.valBuf = hf.valBuf[:0]
	hf.sensible = false
}

// Size calculates the entry size in octets as len(name) + len(value) + 32 overhead (RFC 7541 §4.1).
func (hf *HeaderField) Size() uint32       { return uint32(len(hf.key) + len(hf.value) + 32) } //nolint:gosec
func (hf *HeaderField) Key() string        { return string(hf.key) }
func (hf *HeaderField) Value() string      { return string(hf.value) }
func (hf *HeaderField) KeyBytes() []byte   { return hf.key }
func (hf *HeaderField) ValueBytes() []byte { return hf.value }
func (hf *HeaderField) IsPseudo() bool     { return len(hf.key) > 0 && hf.key[0] == ':' }
func (hf *HeaderField) IsSensible() bool   { return hf.sensible }

func (hf *HeaderField) Set(k, v string) {
	hf.SetKey(k)
	hf.SetValue(v)
}

func (hf *HeaderField) SetBytes(k, v []byte) {
	hf.SetKeyBytes(k)
	hf.SetValueBytes(v)
}

func (hf *HeaderField) SetKey(key string) {
	if ik := rodata.InternKey(key); ik != nil {
		hf.key = ik
		return
	}

	hf.keyBuf = append(hf.keyBuf[:0], key...)
	hf.key = hf.keyBuf
}

func (hf *HeaderField) SetValue(value string) {
	if iv := rodata.InternValue(value); iv != nil {
		hf.value = iv
		return
	}

	hf.valBuf = append(hf.valBuf[:0], value...)
	hf.value = hf.valBuf
}

func (hf *HeaderField) SetKeyBytes(key []byte) {
	if ik := rodata.InternKeyBytes(key); ik != nil {
		hf.key = ik
		return
	}

	hf.keyBuf = append(hf.keyBuf[:0], key...)
	hf.key = hf.keyBuf
}

func (hf *HeaderField) SetValueBytes(value []byte) {
	if iv := rodata.InternValueBytes(value); iv != nil {
		hf.value = iv
		return
	}

	hf.valBuf = append(hf.valBuf[:0], value...)
	hf.value = hf.valBuf
}

func (hf *HeaderField) CopyTo(other *HeaderField) {
	if ik := rodata.InternKeyBytes(hf.key); ik != nil {
		other.key = ik
	} else {
		other.keyBuf = append(other.keyBuf[:0], hf.key...)
		other.key = other.keyBuf
	}

	if iv := rodata.InternValueBytes(hf.value); iv != nil {
		other.value = iv
	} else {
		other.valBuf = append(other.valBuf[:0], hf.value...)
		other.value = other.valBuf
	}

	other.sensible = hf.sensible
}

func (hf *HeaderField) AppendBytes(dst []byte) []byte {
	dst = append(dst, hf.key...)
	dst = append(dst, ':', ' ')
	dst = append(dst, hf.value...)

	return dst
}

var hpackStorage = pool.NewPerPStorage(func() *HPACK {
	return &HPACK{
		maxTableSize: defaultHeaderTableSize,
		dynamic:      make([]*HeaderField, 0, 16),
	}
})

// HPACK manages static and dynamic header compression tables and HPACK encoding/decoding operations (RFC 7541).
type HPACK struct {
	DisableCompression  bool
	DisableDynamicTable bool
	dynamic             []*HeaderField
	maxTableSize        uint32
	dynamicSize         uint32
}

// AcquireHPACK retrieves an initialized HPACK context from memory pools.
func AcquireHPACK() *HPACK {
	hp := hpackStorage.Get()
	hp.Reset()

	return hp
}

// ReleaseHPACK returns an HPACK context to memory pools after clearing dynamic state.
func ReleaseHPACK(hp *HPACK) {
	if hp != nil {
		hpackStorage.Put(hp)
	}
}

func (hp *HPACK) Reset() {
	hp.releaseDynamic()
	hp.maxTableSize = defaultHeaderTableSize
	hp.dynamicSize = 0
	hp.DisableCompression = false
	hp.DisableDynamicTable = false
}

func (hp *HPACK) releaseDynamic() {
	for _, hf := range hp.dynamic {
		ReleaseHeaderField(hf)
	}

	hp.dynamic = hp.dynamic[:0]
	hp.dynamicSize = 0
}

// SetMaxTableSize updates the maximum dynamic table size capacity (RFC 7541 §4.2 & §6.3).
func (hp *HPACK) SetMaxTableSize(size uint32) {
	hp.maxTableSize = size
	if hp.dynamicSize > size {
		hp.shrink()
	}
}

// DynamicSize computes total dynamic table memory consumption in octets (RFC 7541 §4.1).
func (hp *HPACK) DynamicSize() uint32 {
	return hp.dynamicSize
}

// addDynamic inserts a new header field into the dynamic table with FIFO ordering and eviction (RFC 7541 §2.3.2 & §4.4).
func (hp *HPACK) addDynamic(hf *HeaderField) {
	hf2 := AcquireHeaderField()
	hf.CopyTo(hf2)
	hp.dynamic = append(hp.dynamic, hf2)
	hp.dynamicSize += hf2.Size()
	hp.shrink()
}

// shrink evicts oldest entries from the dynamic table until size is within maxTableSize limit (RFC 7541 §4.3 & §4.4).
func (hp *HPACK) shrink() {
	var n int

	for n = 0; n < len(hp.dynamic) && hp.dynamicSize > hp.maxTableSize; n++ {
		hp.dynamicSize -= hp.dynamic[n].Size()
	}

	if n != 0 {
		for i := range n {
			ReleaseHeaderField(hp.dynamic[i])
		}

		hp.dynamic = append(hp.dynamic[:0], hp.dynamic[n:]...)
	}
}

// peek resolves an 1-based index against the unified index address space (RFC 7541 §2.3.3: static 1..61, dynamic 62+).
func (hp *HPACK) peek(n uint64) *HeaderField {
	if n < maxIndex {
		idx := int(n - 1)
		if idx < 0 || idx >= len(staticTable) {
			return nil
		}

		return staticTable[idx]
	}

	idx := len(hp.dynamic) - int(n-maxIndex) - 1 //nolint:gosec
	if idx < 0 || idx >= len(hp.dynamic) {
		return nil
	}

	return hp.dynamic[idx]
}

//go:inline
//go:nosplit
func fastStaticLookup(key, val []byte) (n uint64, fullMatch, found bool) {
	if len(key) == 0 {
		return 0, false, false
	}

	switch key[0] {
	case ':':
		switch string(key) {
		case ":authority":
			return 1, false, true
		case ":method":
			if bytes.Equal(val, []byte("GET")) {
				return 2, true, true
			}

			if bytes.Equal(val, []byte("POST")) {
				return 3, true, true
			}

			return 2, false, true

		case ":path":
			if bytes.Equal(val, []byte("/")) {
				return 4, true, true
			}

			if bytes.Equal(val, []byte("/index.html")) {
				return 5, true, true
			}

			return 4, false, true

		case ":scheme":
			if bytes.Equal(val, []byte("http")) {
				return 6, true, true
			}

			if bytes.Equal(val, []byte("https")) {
				return 7, true, true
			}

			return 7, false, true

		case ":status":
			if bytes.Equal(val, []byte("200")) {
				return 8, true, true
			}

			return 8, false, true
		}

	case 'a':
		if bytes.Equal(key, []byte("accept-encoding")) {
			if bytes.Equal(val, []byte("gzip, deflate")) {
				return 16, true, true
			}

			return 16, false, true
		}

		if bytes.Equal(key, []byte("accept")) {
			return 19, false, true
		}

	case 'c':
		if bytes.Equal(key, []byte("content-type")) {
			return 31, false, true
		}

		if bytes.Equal(key, []byte("cookie")) {
			return 32, false, true
		}

		if bytes.Equal(key, []byte("content-length")) {
			return 28, false, true
		}

		if bytes.Equal(key, []byte("content-encoding")) {
			return 27, false, true
		}

	case 'h':
		if bytes.Equal(key, []byte("host")) {
			return 38, false, true
		}
	case 'u':
		if bytes.Equal(key, []byte("user-agent")) {
			return 58, false, true
		}
	}

	return 0, false, false
}

func (hp *HPACK) search(hf *HeaderField) (n uint64, fullMatch bool) {
	for i, hf2 := range hp.dynamic {
		if bytes.Equal(hf.key, hf2.key) && bytes.Equal(hf.value, hf2.value) {
			return uint64(maxIndex + len(hp.dynamic) - i - 1), true
		}
	}

	if idx, full, found := fastStaticLookup(hf.key, hf.value); found {
		if full {
			return idx, true
		}

		n = idx
	}

	for i, hf2 := range staticTable {
		if !bytes.Equal(hf.key, hf2.key) {
			continue
		}

		if bytes.Equal(hf.value, hf2.value) {
			return uint64(i + 1), true
		}

		if n == 0 {
			n = uint64(i + 1)
		}
	}

	return n, false
}

// DecodeAll decodes all header fields from b into dst slice.
func (hp *HPACK) DecodeAll(dst []*HeaderField, b []byte) ([]*HeaderField, error) {
	for len(b) > 0 {
		hf := AcquireHeaderField()
		rem, err := hp.Next(hf, b)
		if err != nil {
			ReleaseHeaderField(hf)
			return dst, err
		}
		if hf.Empty() {
			ReleaseHeaderField(hf)
		} else {
			dst = append(dst, hf)
		}
		b = rem
	}

	return dst, nil
}

// Next parses the next HPACK-encoded header field from byte stream b (RFC 7541 §3.2 & §6).
func (hp *HPACK) Next(hf *HeaderField, b []byte) ([]byte, error) {
	for len(b) > 0 {
		c := b[0]
		switch {
		case c&indexByte == indexByte:
			// RFC 7541 §6.1: Indexed Header Field Representation ('1' 1-bit prefix)
			return hp.decodeIndexed(hf, b)
		case c&literalByte == literalByte:
			// RFC 7541 §6.2.1: Literal Header Field with Incremental Indexing ('01' 2-bit prefix)
			return hp.decodeLiteralIndexed(hf, b)
		case c&noIndexByte == 16:
			// RFC 7541 §6.2.3: Literal Header Field Never Indexed ('0001' 4-bit prefix)
			hf.sensible = true
			return hp.decodeLiteralNoIndex(hf, b)
		case c&noIndexByte == 0:
			// RFC 7541 §6.2.2: Literal Header Field without Indexing ('0000' 4-bit prefix)
			return hp.decodeLiteralNoIndex(hf, b)
		case c&32 == 32:
			// RFC 7541 §6.3: Dynamic Table Size Update ('001' 3-bit prefix)
			var n uint64

			b, n = readInt(5, b)
			hp.maxTableSize = uint32(n) //nolint:gosec
			hp.shrink()
		}
	}

	return b, nil
}

// decodeIndexed decodes an indexed header field representation (RFC 7541 §6.1).
func (hp *HPACK) decodeIndexed(hf *HeaderField, b []byte) ([]byte, error) {
	b, n := readInt(7, b)

	if n == 0 {
		// RFC 7541 §6.1: Index value of 0 is not used and MUST be treated as a decoding error.
		return b, NewError(FlowControlError, "index value of 0 is forbidden (RFC 7541 §6.1)")
	}

	hf2 := hp.peek(n)
	if hf2 == nil {
		return b, NewError(FlowControlError, fmt.Sprintf("index field not found: %d (RFC 7541 §2.3.3)", n))
	}

	hf2.CopyTo(hf)

	return b, nil
}

// decodeLiteralIndexed decodes a literal header field with incremental indexing (RFC 7541 §6.2.1).
func (hp *HPACK) decodeLiteralIndexed(hf *HeaderField, b []byte) ([]byte, error) {
	c := b[0]

	var (
		n   uint64
		err error
	)

	var stackBuf [512]byte

	if c != 64 {
		b, n = readInt(6, b)

		hf2 := hp.peek(n)
		if hf2 == nil {
			return b, NewError(FlowControlError, fmt.Sprintf("literal indexed field not found: %d", n))
		}

		hf.SetKeyBytes(hf2.KeyBytes())
	} else {
		b = b[1:]
		dst := stackBuf[:0]

		b, dst, err = readString(dst, b)
		if err != nil {
			return b, err
		}

		hf.SetKeyBytes(dst)
	}

	dst := stackBuf[:0]

	b, dst, err = readString(dst, b)
	if err != nil {
		return b, err
	}

	hf.SetValueBytes(dst)
	hp.addDynamic(hf)

	return b, nil
}

// decodeLiteralNoIndex decodes a literal header field without indexing or never indexed (RFC 7541 §6.2.2 & §6.2.3).
func (hp *HPACK) decodeLiteralNoIndex(hf *HeaderField, b []byte) ([]byte, error) {
	c := b[0]

	var (
		n   uint64
		err error
	)

	var stackBuf [512]byte

	if c&15 != 0 {
		b, n = readInt(4, b)

		hf2 := hp.peek(n)
		if hf2 == nil {
			return b, NewError(FlowControlError, fmt.Sprintf("non-indexed field not found: %d", n))
		}

		hf.SetKeyBytes(hf2.key)
	} else {
		b = b[1:]
		dst := stackBuf[:0]

		b, dst, err = readString(dst, b)
		if err != nil {
			return b, err
		}

		hf.SetKeyBytes(dst)
	}

	dst := stackBuf[:0]

	b, dst, err = readString(dst, b)
	if err != nil {
		return b, err
	}

	hf.SetValueBytes(dst)

	return b, nil
}

func (hp *HPACK) AppendHeaderField(h *Headers, hf *HeaderField, store bool) {
	h.rawHeaders = hp.AppendHeader(h.rawHeaders, hf, store)
}

// AppendHeader encodes hf into dst using the optimal HPACK binary representation (RFC 7541 §6).
func (hp *HPACK) AppendHeader(dst []byte, hf *HeaderField, store bool) []byte {
	c := !hp.DisableCompression
	index, fullMatch := hp.search(hf)

	if fullMatch {
		// RFC 7541 §6.1: Indexed header field representation
		dst = append(dst, indexByte)
		return appendInt(dst, 7, index)
	}

	var bits uint8

	switch {
	case hf.sensible:
		// RFC 7541 §6.2.3 & §7.1.3: Literal Header Field Never Indexed (protects confidential headers)
		c = false
		bits = 4

		dst = append(dst, 16)

	case !store || hp.DisableDynamicTable:
		// RFC 7541 §6.2.2: Literal Header Field without Indexing
		bits = 4

		dst = append(dst, 0)

	default:
		// RFC 7541 §6.2.1: Literal Header Field with Incremental Indexing (0x40)
		bits = 6

		dst = append(dst, literalByte)

		hp.addDynamic(hf)
	}

	if index > 0 {
		dst = appendInt(dst, bits, index)
	} else {
		dst = appendInt(dst, bits, 0)
		dst = appendString(dst, hf.key, c)
	}

	dst = appendString(dst, hf.value, c)

	return dst
}

var byteStorage = pool.NewPerPStorage(func() *[]byte {
	b := make([]byte, 0, 512)
	return &b
})

// readInt decodes an unsigned variable-length integer with an N-bit prefix (RFC 7541 §5.1).
func readInt(n int, b []byte) ([]byte, uint64) {
	if hasVectorInt {
		return vectorReadInt(n, b)
	}

	return readIntFallback(n, b)
}

func readIntFallback(n int, b []byte) ([]byte, uint64) {
	if len(b) == 0 {
		return b, 0
	}

	b0 := byte(1<<n - 1)
	if b0&b[0] != b0 {
		return b[1:], uint64(b[0] & b0)
	}

	var nn uint64

	i := 1

	for i < len(b) {
		c := b[i]
		if i > 10 {
			// RFC 7541 §5.1 & §7.4: Integer encodings that exceed implementation limits MUST be bounded.
			break
		}

		nn |= uint64(c&127) << ((i - 1) * 7)
		i++

		if c&128 != 128 {
			break
		}
	}

	return b[i:], nn + uint64(b0)
}

// AppendInt encodes an unsigned variable-length integer index using an N-bit prefix into dst (RFC 7541 §5.1).
func AppendInt(dst []byte, bits uint8, index uint64) []byte {
	return appendInt(dst, bits, index)
}

// ReadInt decodes an unsigned variable-length integer from b using an N-bit prefix (RFC 7541 §5.1).
func ReadInt(n int, b []byte) ([]byte, uint64) {
	return readInt(n, b)
}

// appendInt encodes an unsigned variable-length integer index using an N-bit prefix into dst (RFC 7541 §5.1).
func appendInt(dst []byte, bits uint8, index uint64) []byte {
	if hasVectorInt {
		return vectorAppendInt(dst, bits, index)
	}

	if len(dst) == 0 {
		dst = append(dst, 0)
	}

	b0 := uint64(1<<bits - 1)

	if index <= b0 { //nolint:gosec
		dst[len(dst)-1] |= byte(index)
		return dst
	}

	dst[len(dst)-1] |= byte(b0) //nolint:gosec
	index -= b0

	for index != 0 {
		dst = append(dst, 128|byte(index&127))
		index >>= 7
	}

	dst[len(dst)-1] &= 127

	return dst
}

// readString decodes an HPACK string literal representation with optional Huffman decoding (RFC 7541 §5.2).
func readString(dst, b []byte) ([]byte, []byte, error) {
	if len(b) == 0 {
		return b, dst, ErrMalformedString
	}

	mustDecode := b[0]&128 == 128

	b, n := readInt(7, b)
	if uint64(len(b)) < n {
		return b, dst, ErrUnexpectedSize
	}

	if mustDecode {
		dst = HuffmanDecode(dst, b[:n])
	} else {
		dst = append(dst, b[:n]...)
	}

	return b[n:], dst, nil
}

// appendString encodes a string literal with optional static Huffman coding and 7-bit length prefix (RFC 7541 §5.2).
func appendString(dst, src []byte, encode bool) []byte {
	var (
		bufPtr   *[]byte
		payload  []byte
		stackBuf [512]byte
	)

	switch {
	case !encode:
		payload = src
	case len(src) <= 256:
		payload = HuffmanEncode(stackBuf[:0], src)
	default:
		bufPtr = byteStorage.Get()
		payload = HuffmanEncode((*bufPtr)[:0], src)
	}

	payloadLen := uint64(len(payload))
	hBitIdx := len(dst)
	dst = append(dst, 0)

	dst = appendInt(dst, 7, payloadLen)
	dst = append(dst, payload...)

	if encode {
		if bufPtr != nil {
			*bufPtr = payload
			byteStorage.Put(bufPtr)
		}

		dst[hBitIdx] |= 0x80
	}

	return dst
}

func init() {
	for _, hf := range staticTable {
		hf.static = true
	}
}

// staticTable defines the 61 predefined HTTP/2 header field entries specified in RFC 7541 Appendix A.
var staticTable = []*HeaderField{
	{key: []byte(":authority")},
	{key: []byte(":method"), value: []byte("GET")},
	{key: []byte(":method"), value: []byte("POST")},
	{key: []byte(":path"), value: []byte("/")},
	{key: []byte(":path"), value: []byte("/index.html")},
	{key: []byte(":scheme"), value: []byte("http")},
	{key: []byte(":scheme"), value: []byte("https")},
	{key: []byte(":status"), value: []byte("200")},
	{key: []byte(":status"), value: []byte("204")},
	{key: []byte(":status"), value: []byte("206")},
	{key: []byte(":status"), value: []byte("304")},
	{key: []byte(":status"), value: []byte("400")},
	{key: []byte(":status"), value: []byte("404")},
	{key: []byte(":status"), value: []byte("500")},
	{key: []byte("accept-charset")},
	{key: []byte("accept-encoding"), value: []byte("gzip, deflate")},
	{key: []byte("accept-language")},
	{key: []byte("accept-ranges")},
	{key: []byte("accept")},
	{key: []byte("access-control-allow-origin")},
	{key: []byte("age")},
	{key: []byte("allow")},
	{key: []byte("authorization")},
	{key: []byte("cache-control")},
	{key: []byte("content-disposition")},
	{key: []byte("content-encoding")},
	{key: []byte("content-language")},
	{key: []byte("content-length")},
	{key: []byte("content-location")},
	{key: []byte("content-range")},
	{key: []byte("content-type")},
	{key: []byte("cookie")},
	{key: []byte("date")},
	{key: []byte("etag")},
	{key: []byte("expect")},
	{key: []byte("expires")},
	{key: []byte("from")},
	{key: []byte("host")},
	{key: []byte("if-match")},
	{key: []byte("if-modified-since")},
	{key: []byte("if-none-match")},
	{key: []byte("if-range")},
	{key: []byte("if-unmodified-since")},
	{key: []byte("last-modified")},
	{key: []byte("link")},
	{key: []byte("location")},
	{key: []byte("max-forwards")},
	{key: []byte("proxy-authenticate")},
	{key: []byte("proxy-authorization")},
	{key: []byte("range")},
	{key: []byte("referer")},
	{key: []byte("refresh")},
	{key: []byte("retry-after")},
	{key: []byte("server")},
	{key: []byte("set-cookie")},
	{key: []byte("strict-transport-security")},
	{key: []byte("transfer-encoding")},
	{key: []byte("user-agent")},
	{key: []byte("vary")},
	{key: []byte("via")},
	{key: []byte("www-authenticate")},
}
