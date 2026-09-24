// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/tests/testutil"
	"github.com/lemon4ksan/mach/proto/headkit"
	"github.com/lemon4ksan/mach/qpack"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	coreh3 "github.com/lemon4ksan/mach/proto/h3"
	mach "github.com/lemon4ksan/mach/proto/http"
)

// -----------------------------------------------------------------------------
// Test Helpers & Mock Infrastructure
// -----------------------------------------------------------------------------

// stressHeadersHandler implements qpack.HeadersHandlerInterface for test validation.
type stressHeadersHandler struct {
	mu        sync.Mutex
	headers   []qpack.HeaderField
	completed bool
	errCode   uint64
	errMsg    string
}

func (h *stressHeadersHandler) OnHeaderDecoded(name, value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.headers = append(h.headers, qpack.HeaderField{Name: name, Value: value})
}

func (h *stressHeadersHandler) OnDecodingCompleted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.completed = true
}

func (h *stressHeadersHandler) OnDecodingErrorDetected(errorCode uint64, errorMessage string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.errCode = errorCode
	h.errMsg = errorMessage
}

func (h *stressHeadersHandler) Headers() []qpack.HeaderField {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]qpack.HeaderField, len(h.headers))
	copy(cp, h.headers)
	return cp
}

func (h *stressHeadersHandler) Completed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.completed
}

func (h *stressHeadersHandler) ErrorCode() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.errCode
}

func (h *stressHeadersHandler) ErrorMsg() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.errMsg
}

// stressSenderDelegate implements qpack.StreamSenderDelegate for buffering and stream control.
type stressSenderDelegate struct {
	mu      sync.Mutex
	buf     []byte
	onWrite func([]byte)
}

func (s *stressSenderDelegate) WriteStreamData(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, data...)
	if s.onWrite != nil {
		s.onWrite(data)
	}
}

func (s *stressSenderDelegate) NumBytesBuffered() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.buf))
}

func (s *stressSenderDelegate) Drain() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.buf
	s.buf = nil
	return out
}

// qpackRoundtripHarness simulates synchronized virtual encoder and decoder control streams.
type qpackRoundtripHarness struct {
	t         *testing.T
	encoder   *qpack.Encoder
	decoder   *qpack.Decoder
	encSender *stressSenderDelegate
	decSender *stressSenderDelegate
}

func newQPACKRoundtripHarness(t *testing.T, maxCapacity, maxBlocked uint64) *qpackRoundtripHarness {
	h := &qpackRoundtripHarness{
		t:         t,
		encSender: &stressSenderDelegate{},
		decSender: &stressSenderDelegate{},
	}
	h.encoder = qpack.NewEncoderWithDefaults(nil)
	h.encoder.SetMaximumDynamicTableCapacity(maxCapacity)
	h.encoder.SetMaximumBlockedStreams(maxBlocked)
	h.encoder.SetStreamSenderDelegate(h.encSender)

	h.decoder = qpack.NewDecoder(maxCapacity, maxBlocked, nil)
	h.decoder.SetStreamSenderDelegate(h.decSender)

	if maxCapacity > 0 {
		h.encoder.SetDynamicTableCapacity(maxCapacity)
		h.decoder.SetDynamicTableCapacity(maxCapacity)
	}

	h.encSender.onWrite = func(data []byte) {
		h.decoder.EncoderStreamReceiver().Decode(data)
	}
	h.decSender.onWrite = func(data []byte) {
		h.encoder.DecoderStreamReceiver().Decode(data)
	}

	return h
}

func (h *qpackRoundtripHarness) Roundtrip(streamID uint64, fields []qpack.HeaderField) []qpack.HeaderField {
	h.t.Helper()
	block := h.encoder.EncodeHeaderList(streamID, fields, nil)
	handler := &stressHeadersHandler{}
	pDec := h.decoder.CreateProgressiveDecoder(streamID, handler)
	pDec.Decode(block)
	pDec.EndHeaderBlock()
	h.decoder.FlushDecoderStream()

	require.True(h.t, handler.Completed(), "progressive decoder must complete")
	require.Equal(h.t, uint64(0), handler.ErrorCode(), "no decode errors expected")
	return handler.Headers()
}

// =============================================================================
// Feature F9: QPACK Dynamic Table Capacity Limits (0 to 65,536 Bytes)
// =============================================================================

// TestQPACK_F9_DynamicTableCapacity_BoundaryLimits verifies dynamic table capacity
// boundary enforcement at 0, 31, 32, 38, 4096, 65535, and 65536 bytes.
func TestQPACK_F9_DynamicTableCapacity_BoundaryLimits(t *testing.T) {
	t.Parallel()

	// 1. Capacity 0 boundary
	table0 := qpack.NewEncoderHeaderTable()
	require.True(t, table0.SetMaximumDynamicTableCapacity(0))
	require.True(t, table0.SetDynamicTableCapacity(0))
	assert.Equal(t, uint64(0), table0.MaxEntries())
	assert.Equal(t, uint64(0), table0.DynamicTableCapacity())
	assert.False(t, table0.EntryFitsDynamicTableCapacity("", ""))
	assert.False(t, table0.EntryFitsDynamicTableCapacity("a", "b"))

	// 2. Minimum entry boundaries: 31 vs 32 bytes (32-byte RFC 9204 overhead)
	table32 := qpack.NewEncoderHeaderTable()
	require.True(t, table32.SetMaximumDynamicTableCapacity(32))
	require.True(t, table32.SetDynamicTableCapacity(32))
	assert.Equal(t, uint64(1), table32.MaxEntries())
	assert.True(t, table32.EntryFitsDynamicTableCapacity("", ""))   // 0 + 0 + 32 = 32
	assert.False(t, table32.EntryFitsDynamicTableCapacity("a", "")) // 1 + 0 + 32 = 33 > 32

	table31 := qpack.NewEncoderHeaderTable()
	require.True(t, table31.SetMaximumDynamicTableCapacity(31))
	require.True(t, table31.SetDynamicTableCapacity(31))
	assert.Equal(t, uint64(0), table31.MaxEntries())
	assert.False(t, table31.EntryFitsDynamicTableCapacity("", ""))

	// 3. Exact single entry boundary: 38 bytes
	table38 := qpack.NewEncoderHeaderTable()
	require.True(t, table38.SetMaximumDynamicTableCapacity(38))
	require.True(t, table38.SetDynamicTableCapacity(38))
	assert.True(t, table38.EntryFitsDynamicTableCapacity("foo", "bar")) // 3 + 3 + 32 = 38
	idx := table38.InsertEntry("foo", "bar")
	assert.Equal(t, uint64(0), idx)
	assert.Equal(t, uint64(38), table38.DynamicTableSize())

	// 4. Power-of-two and 16-bit boundaries: 4096, 65535, and 65536
	table4K := qpack.NewEncoderHeaderTable()
	require.True(t, table4K.SetMaximumDynamicTableCapacity(4096))
	assert.Equal(t, uint64(4096/32), table4K.MaxEntries()) // 128
	require.True(t, table4K.SetDynamicTableCapacity(4096))
	assert.Equal(t, uint64(4096), table4K.DynamicTableCapacity())

	table64K := qpack.NewEncoderHeaderTable()
	require.True(t, table64K.SetMaximumDynamicTableCapacity(65536))
	assert.Equal(t, uint64(65536/32), table64K.MaxEntries()) // 2048
	require.True(t, table64K.SetDynamicTableCapacity(65535))
	assert.Equal(t, uint64(65535), table64K.DynamicTableCapacity())
	require.True(t, table64K.SetDynamicTableCapacity(65536))
	assert.Equal(t, uint64(65536), table64K.DynamicTableCapacity())

	// 5. Clamping: capacity exceeding maximumDynamicTableCapacity MUST return false
	assert.False(t, table64K.SetDynamicTableCapacity(65537))
	assert.False(t, table64K.SetDynamicTableCapacity(100000))

	// 6. Mutating maximum capacity once established MUST return false
	assert.False(t, table64K.SetMaximumDynamicTableCapacity(32768))
	assert.True(t, table64K.SetMaximumDynamicTableCapacity(65536)) // identical value allowed
}

// TestQPACK_F9_DynamicTableCapacity_DynamicExpansionAndContraction verifies dynamic table
// capacity cycling (4096 -> 65536 -> 1000 -> 0 -> 4096) under active entry presence.
func TestQPACK_F9_DynamicTableCapacity_DynamicExpansionAndContraction(t *testing.T) {
	t.Parallel()

	table := qpack.NewEncoderHeaderTable()
	require.True(t, table.SetMaximumDynamicTableCapacity(65536))
	require.True(t, table.SetDynamicTableCapacity(4096))

	// Insert 50 entries of size 40 bytes each = 2000 bytes (key-XXX is 7 bytes, val is 3 bytes, overhead 32)
	for i := 0; i < 50; i++ {
		table.InsertEntry(fmt.Sprintf("key-%03d", i), "val")
	}
	assert.Equal(t, uint64(50), table.InsertedEntryCount())
	assert.Equal(t, uint64(0), table.DroppedEntryCount())
	assert.Equal(t, uint64(50*42), table.DynamicTableSize()) // 7 + 3 + 32 = 42

	// Expand to 65,536 bytes
	require.True(t, table.SetDynamicTableCapacity(65536))
	assert.Equal(t, uint64(65536), table.DynamicTableCapacity())
	assert.Equal(t, uint64(50*42), table.DynamicTableSize())
	assert.Equal(t, uint64(0), table.DroppedEntryCount())

	// Contract to 1000 bytes (holds 23 entries of 42 bytes = 966 bytes <= 1000)
	require.True(t, table.SetDynamicTableCapacity(1000))
	assert.Equal(t, uint64(1000), table.DynamicTableCapacity())
	assert.True(t, table.DynamicTableSize() <= 1000)
	assert.Equal(t, uint64(50-23), table.DroppedEntryCount()) // 27 entries dropped
	assert.Equal(t, uint64(50), table.InsertedEntryCount())

	// Contract to 0 bytes: complete eviction
	require.True(t, table.SetDynamicTableCapacity(0))
	assert.Equal(t, uint64(0), table.DynamicTableSize())
	assert.Equal(t, uint64(50), table.DroppedEntryCount())

	// Re-expand to 4096 bytes: accept new entries cleanly
	require.True(t, table.SetDynamicTableCapacity(4096))
	idx := table.InsertEntry("new-key", "new-val")
	assert.Equal(t, uint64(50), idx) // absolute index continues monotonically
	assert.Equal(t, uint64(1), table.InsertedEntryCount()-table.DroppedEntryCount())
}

// TestQPACK_F9_RFC9204_32ByteOverhead_ExactSizing verifies exact byte calculation
// of len(name) + len(value) + 32 and eviction behavior at the single-byte boundary.
func TestQPACK_F9_RFC9204_32ByteOverhead_ExactSizing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		val      string
		expected uint64
	}{
		{"", "", 32},
		{"a", "", 33},
		{"", "b", 33},
		{"custom-header", "custom-value", uint64(13 + 12 + 32)},                      // 57
		{strings.Repeat("k", 100), strings.Repeat("v", 200), uint64(100 + 200 + 32)}, // 332
	}

	for _, tc := range testCases {
		entry := qpack.NewEntry(tc.name, tc.val)
		assert.Equal(t, tc.expected, entry.Size())
		assert.Equal(t, tc.expected, qpack.EntrySize(tc.name, tc.val))
	}

	// Exact capacity filling without eviction:
	// Two entries of 36 bytes each (k1, v1: 2 + 2 + 32 = 36). Total 72 bytes.
	table := qpack.NewEncoderHeaderTable()
	require.True(t, table.SetMaximumDynamicTableCapacity(100))
	require.True(t, table.SetDynamicTableCapacity(72))

	table.InsertEntry("k1", "v1") // 36 bytes
	table.InsertEntry("k2", "v2") // 36 bytes; total 72 <= 72
	assert.Equal(t, uint64(0), table.DroppedEntryCount())
	assert.Equal(t, uint64(72), table.DynamicTableSize())

	// Inserting an entry of size 36 triggers eviction of 1 entry because 72 + 36 = 108 > 72.
	table.InsertEntry("k3", "v3")
	assert.Equal(t, uint64(1), table.DroppedEntryCount())
	assert.Equal(t, uint64(72), table.DynamicTableSize())
}

// TestQPACK_F9_ZeroCapacity_PureLiteralFallback verifies that when maximum dynamic
// table capacity is 0, the encoder encodes all headers using literal representations
// without errors, panics, or memory allocations in the dynamic table.
func TestQPACK_F9_ZeroCapacity_PureLiteralFallback(t *testing.T) {
	t.Parallel()

	codec := coreh3.NewQPACKCodecWithOptions(0, 0, nil)
	assert.Equal(t, uint64(0), codec.Encoder().HeaderTable().MaximumDynamicTableCapacity())

	headers := []qpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "https"},
		{Name: ":authority", Value: "example.com"},
		{Name: ":path", Value: "/zero-capacity"},
		{Name: "x-custom-one", Value: "value-one"},
		{Name: "x-custom-two", Value: "value-two"},
	}

	block := codec.Encoder().EncodeHeaderList(0, headers, nil)
	require.NotEmpty(t, block)

	// Dynamic table must remain completely empty
	assert.Equal(t, uint64(0), codec.Encoder().HeaderTable().DynamicTableSize())
	assert.Equal(t, uint64(0), codec.Encoder().HeaderTable().InsertedEntryCount())

	// Decode using Decoder configured with capacity 0
	handler := &stressHeadersHandler{}
	prog := codec.Decoder().CreateProgressiveDecoder(0, handler)
	prog.Decode(block)
	prog.EndHeaderBlock()

	require.True(t, handler.Completed())
	require.Equal(t, uint64(0), handler.ErrorCode())
	assert.Equal(t, len(headers), len(handler.Headers()))
}

// =============================================================================
// Feature F10: Heavy Varying Header Eviction & Turnover
// =============================================================================

// TestQPACK_F10_HeavyVaryingHeaders_RingBufferFIFOEviction_100UniqueKeys inserts 150+ unique
// header keys into a bounded dynamic table (capacity 1024 bytes), verifying strict FIFO
// eviction order, monotonic absolute index progression, and clean unreachability of evicted entries.
func TestQPACK_F10_HeavyVaryingHeaders_RingBufferFIFOEviction_100UniqueKeys(t *testing.T) {
	t.Parallel()

	const tableCap uint64 = 1024
	table := qpack.NewEncoderHeaderTable()
	require.True(t, table.SetMaximumDynamicTableCapacity(tableCap))
	require.True(t, table.SetDynamicTableCapacity(tableCap))

	const numUniqueHeaders = 150
	type insertedHeader struct {
		name  string
		val   string
		index uint64
		size  uint64
	}
	history := make([]insertedHeader, numUniqueHeaders)

	for i := 0; i < numUniqueHeaders; i++ {
		name := fmt.Sprintf("x-unique-header-%03d", i) // 19 bytes
		val := fmt.Sprintf("val-%03d", i)              // 7 bytes
		size := qpack.EntrySize(name, val)             // 19 + 7 + 32 = 58 bytes

		idx := table.InsertEntry(name, val)
		history[i] = insertedHeader{name: name, val: val, index: idx, size: size}

		assert.True(t, table.DynamicTableSize() <= tableCap,
			fmt.Sprintf("table size %d exceeded capacity %d at step %d", table.DynamicTableSize(), tableCap, i))
	}

	assert.Equal(t, uint64(numUniqueHeaders), table.InsertedEntryCount())
	assert.True(t, table.DroppedEntryCount() > 0, "eviction must have occurred")

	dropped := table.DroppedEntryCount()
	// 1. Verify evicted entries are no longer retrievable
	for i := uint64(0); i < dropped; i++ {
		res := table.FindHeaderField(history[i].name, history[i].val)
		assert.True(t, res.Match == qpack.MatchTypeNoMatch || res.Index > i,
			fmt.Sprintf("evicted entry %s at index %d should not match old index", history[i].name, i))
	}

	// 2. Verify surviving newest entries are present and retrievable
	for i := dropped; i < uint64(numUniqueHeaders); i++ {
		res := table.FindHeaderField(history[i].name, history[i].val)
		assert.Equal(t, qpack.MatchTypeNameAndValue, res.Match)
		assert.Equal(t, history[i].index, res.Index)
		assert.False(t, res.IsStatic)
	}
}

// TestQPACK_F10_DuplicateHeadersAndLargeValues verifies handling of duplicate header names
// (e.g. 20 Set-Cookie entries), identical (name, value) pairs, and oversized header values (> capacity)
// without panic or table corruption.
func TestQPACK_F10_DuplicateHeadersAndLargeValues(t *testing.T) {
	t.Parallel()

	const tableCap uint64 = 512
	table := qpack.NewEncoderHeaderTable()
	require.True(t, table.SetMaximumDynamicTableCapacity(tableCap))
	require.True(t, table.SetDynamicTableCapacity(tableCap))

	// 1. Insert duplicate custom header names with varying values (not in static table)
	for i := 0; i < 20; i++ {
		table.InsertEntry("x-duplicate-header", fmt.Sprintf("session-%d=val-%d", i, i))
	}

	// Name lookup must return the latest inserted entry's index in dynamic table
	nameRes := table.FindHeaderName("x-duplicate-header")
	assert.Equal(t, qpack.MatchTypeName, nameRes.Match)
	assert.False(t, nameRes.IsStatic)
	assert.Equal(t, table.InsertedEntryCount()-1, nameRes.Index)

	// Static table lookup verification: static names take precedence per RFC 9204
	staticRes := table.FindHeaderName("set-cookie")
	assert.Equal(t, qpack.MatchTypeName, staticRes.Match)
	assert.True(t, staticRes.IsStatic)
	assert.Equal(t, uint64(14), staticRes.Index)

	// 2. Repeated identical entry
	table.InsertEntry("x-repeat", "identical")
	table.InsertEntry("x-repeat", "identical")
	secondIdx := table.InsertedEntryCount() - 1

	exactRes := table.FindHeaderField("x-repeat", "identical")
	assert.Equal(t, qpack.MatchTypeNameAndValue, exactRes.Match)
	assert.Equal(t, secondIdx, exactRes.Index)

	// 3. Oversized entry test via Encoder
	// Setting table capacity to 100. Header of size 150 bytes cannot fit.
	encoder := qpack.NewEncoderWithDefaults(nil)
	encoder.SetMaximumDynamicTableCapacity(100)
	encoder.SetDynamicTableCapacity(100)

	largeVal := strings.Repeat("A", 150)
	headers := []qpack.HeaderField{
		{Name: "x-large-header", Value: largeVal},
	}

	// Must NOT panic, and must encode as literal
	var block []byte
	require.NotPanics(t, func() {
		block = encoder.EncodeHeaderList(0, headers, nil)
	})
	require.NotEmpty(t, block)
	// Dynamic table size must remain 0
	assert.Equal(t, uint64(0), encoder.HeaderTable().DynamicTableSize())
}

// TestQPACK_F10_HighTurnover_Roundtrip_ContinuousChurn executes continuous roundtrip
// encoding and progressive decoding across 200 successive header blocks with 15 varied
// headers each, verifying 100% byte fidelity and zero data corruption under heavy churn.
func TestQPACK_F10_HighTurnover_Roundtrip_ContinuousChurn(t *testing.T) {
	t.Parallel()

	harness := newQPACKRoundtripHarness(t, 4096, 100)

	for blockIdx := 0; blockIdx < 200; blockIdx++ {
		headers := make([]qpack.HeaderField, 15)
		headers[0] = qpack.HeaderField{Name: ":status", Value: "200"}
		headers[1] = qpack.HeaderField{Name: ":method", Value: "GET"}
		headers[2] = qpack.HeaderField{Name: ":scheme", Value: "https"}
		headers[3] = qpack.HeaderField{Name: ":path", Value: fmt.Sprintf("/churn/%d", blockIdx)}

		for i := 4; i < 15; i++ {
			headers[i] = qpack.HeaderField{
				Name:  fmt.Sprintf("x-churn-header-%d-%d", blockIdx, i),
				Value: fmt.Sprintf("churn-val-%d", (blockIdx*17+i*31)%1000),
			}
		}

		decoded := harness.Roundtrip(uint64(blockIdx), headers)
		require.Equal(t, len(headers), len(decoded), fmt.Sprintf("header count mismatch in block %d", blockIdx))
		for i := range headers {
			assert.Equal(t, headers[i].Name, decoded[i].Name)
			assert.Equal(t, headers[i].Value, decoded[i].Value)
		}
	}
}

// TestQPACK_F10_ConcurrentCodecStress_GoroutineRaceSafety executes concurrent encoding
// and decoding on a single shared QPACKCodec under heavy churn across 20 goroutines,
// verifying dual mutex thread-safety and clean race detector execution (-race).
func TestQPACK_F10_ConcurrentCodecStress_GoroutineRaceSafety(t *testing.T) {
	t.Parallel()

	codec := coreh3.NewQPACKCodecWithOptions(4096, 100, nil)

	const (
		numWorkers = 20
		iterations = 50
	)
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*iterations)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				streamID := uint64(workerID*iterations + i)
				var req mach.Request
				req.Header.SetMethod("GET")
				req.SetRequestURI(fmt.Sprintf("https://example.com/w%d/i%d", workerID, i))
				req.Header.Set("X-Worker", fmt.Sprintf("%d", workerID))
				req.Header.Set("X-Iter", fmt.Sprintf("%d", i))
				req.Header.Set("X-Varying", fmt.Sprintf("random-%d", workerID^i))

				var buf bytes.Buffer
				if err := codec.EncodeRequestHeaders(streamID, &buf, &req, nil); err != nil {
					errCh <- fmt.Errorf("w%d i%d encode err: %w", workerID, i, err)
					return
				}

				var respHeader mach.ResponseHeader
				// Encode response
				respBlock := codec.EncodeResponseHeaders(streamID, 200, headkit.NewWithCapacity(4), 0)
				if _, err := codec.DecodeResponseHeaders(streamID, respBlock, &respHeader); err != nil {
					errCh <- fmt.Errorf("w%d i%d decode resp err: %w", workerID, i, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("Concurrent QPACKCodec failure: %v", err)
	}
}

// =============================================================================
// Feature F11: QPACK Blocked Stream Handling Under Load
// =============================================================================

// TestQPACK_F11_BlockedStreamQueue_AccountingAndSaturation tests that the encoder's
// BlockingManager strictly bounds concurrent blocked streams to SETTINGS_QPACK_BLOCKED_STREAMS.
// When quota is reached, novel streams are forbidden from blocking and fall back to literal
// representations, while already-blocked streams can continue referencing entries.
func TestQPACK_F11_BlockedStreamQueue_AccountingAndSaturation(t *testing.T) {
	t.Parallel()

	const maxBlocked = 3
	encSender := &stressSenderDelegate{}
	decSender := &stressSenderDelegate{}

	encoder := qpack.NewEncoderWithDefaults(nil)
	encoder.SetMaximumDynamicTableCapacity(4096)
	encoder.SetDynamicTableCapacity(4096)
	encoder.SetMaximumBlockedStreams(maxBlocked)
	encoder.SetStreamSenderDelegate(encSender)

	decoder := qpack.NewDecoder(4096, maxBlocked, nil)
	decoder.SetDynamicTableCapacity(4096)
	decoder.SetStreamSenderDelegate(decSender)

	bm := encoder.BlockingManager()
	assert.Equal(t, uint64(0), bm.NumBlockedStreams())

	// 1. Saturate quota: streams 10, 20, 30 emit dynamic entries without decoder feedback
	for s := uint64(1); s <= maxBlocked; s++ {
		streamID := s * 10
		require.True(t, bm.BlockingAllowedOnStream(streamID, maxBlocked))

		block := encoder.EncodeHeaderList(streamID, []qpack.HeaderField{
			{Name: fmt.Sprintf("x-quota-%d", s), Value: fmt.Sprintf("val-%d", s)},
		}, nil)
		require.NotEmpty(t, block)
		assert.True(t, bm.IsBlocked(streamID))
	}

	assert.Equal(t, uint64(maxBlocked), bm.NumBlockedStreams())

	// 2. Boundary Check: A novel stream (stream 99) must NOT be permitted to block
	assert.False(t, bm.BlockingAllowedOnStream(99, maxBlocked))

	// Encode on stream 99: encoder MUST refuse to insert into dynamic table and fallback to literal
	block99 := encoder.EncodeHeaderList(99, []qpack.HeaderField{
		{Name: "x-refused-dynamic", Value: "must-be-literal"},
	}, nil)
	require.NotEmpty(t, block99)
	assert.False(t, bm.IsBlocked(99), "stream 99 must NOT be blocked")
	assert.Equal(t, byte(0x00), block99[0], "stream 99 must encode with RIC=0 prefix")
	assert.Equal(t, uint64(maxBlocked), bm.NumBlockedStreams())

	// 3. Re-entrancy Check: An ALREADY BLOCKED stream (e.g. stream 20) CAN reference dynamic entries
	assert.True(t, bm.BlockingAllowedOnStream(20, maxBlocked))
	block20Second := encoder.EncodeHeaderList(20, []qpack.HeaderField{
		{Name: "x-quota-2", Value: "val-2"},
	}, nil)
	require.NotEmpty(t, block20Second)
	assert.Equal(t, uint64(maxBlocked), bm.NumBlockedStreams())

	// 4. Release Quota: Decoder acknowledges stream 10
	encoder.OnHeaderAcknowledgement(10)
	assert.Equal(t, uint64(maxBlocked-1), bm.NumBlockedStreams())
	assert.True(t, bm.BlockingAllowedOnStream(100, maxBlocked))

	// Stream 100 now successfully takes the freed slot
	block100 := encoder.EncodeHeaderList(100, []qpack.HeaderField{
		{Name: "x-quota-100", Value: "val-100"},
	}, nil)
	require.NotEmpty(t, block100)
	assert.True(t, bm.IsBlocked(100))
	assert.Equal(t, uint64(maxBlocked), bm.NumBlockedStreams())
}

// TestQPACK_F11_DecoderOutOfOrder_InFlightDynamicInsertions verifies that when HTTP/3
// request streams arrive out of order (before encoder stream instructions are received),
// the decoder correctly registers observers, queues the streams in blocked state, and
// unblocks them in threshold sequence as dynamic table insertions arrive.
func TestQPACK_F11_DecoderOutOfOrder_InFlightDynamicInsertions(t *testing.T) {
	t.Parallel()

	const maxBlocked = 5
	encSender := &stressSenderDelegate{}
	decSender := &stressSenderDelegate{}

	encoder := qpack.NewEncoderWithDefaults(nil)
	encoder.SetMaximumDynamicTableCapacity(4096)
	encoder.SetDynamicTableCapacity(4096)
	encoder.SetMaximumBlockedStreams(maxBlocked)
	encoder.SetStreamSenderDelegate(encSender)

	decoder := qpack.NewDecoder(4096, maxBlocked, nil)
	decoder.SetDynamicTableCapacity(4096)
	decoder.SetStreamSenderDelegate(decSender)

	// Step 1: Encoder produces 3 header blocks with strictly increasing RIC dependencies
	// Stream 10: RIC=1 ("x-dep-1": "alpha")
	blockA := encoder.EncodeHeaderList(10, []qpack.HeaderField{{Name: "x-dep-1", Value: "alpha"}}, nil)
	// Stream 20: RIC=2 ("x-dep-2": "beta")
	blockB := encoder.EncodeHeaderList(20, []qpack.HeaderField{{Name: "x-dep-2", Value: "beta"}}, nil)
	// Stream 30: RIC=3 ("x-dep-3": "gamma")
	blockC := encoder.EncodeHeaderList(30, []qpack.HeaderField{{Name: "x-dep-3", Value: "gamma"}}, nil)

	// Step 2: Deliver request streams OUT OF ORDER to decoder (30 first, 20 second, 10 third)
	// Note: Encoder stream data has NOT been delivered yet (decoder has InsertedEntryCount = 0)
	handlerC := &stressHeadersHandler{}
	pDecC := decoder.CreateProgressiveDecoder(30, handlerC)
	pDecC.Decode(blockC)
	pDecC.EndHeaderBlock()

	handlerB := &stressHeadersHandler{}
	pDecB := decoder.CreateProgressiveDecoder(20, handlerB)
	pDecB.Decode(blockB)
	pDecB.EndHeaderBlock()

	handlerA := &stressHeadersHandler{}
	pDecA := decoder.CreateProgressiveDecoder(10, handlerA)
	pDecA.Decode(blockA)
	pDecA.EndHeaderBlock()

	// Assert that ALL THREE streams are currently blocked awaiting dynamic entries
	assert.False(t, handlerC.Completed())
	assert.False(t, handlerB.Completed())
	assert.False(t, handlerA.Completed())
	assert.Equal(t, uint64(0), handlerC.ErrorCode())
	assert.Equal(t, uint64(0), handlerB.ErrorCode())
	assert.Equal(t, uint64(0), handlerA.ErrorCode())

	// Step 3: Deliver encoder stream instructions step-by-step
	// Instruction 1: inserts "x-dep-1" -> InsertedEntryCount reaches 1
	decoder.InsertWithoutNameReference("x-dep-1", "alpha")
	// Stream 10 (RIC=1) must unblock immediately!
	assert.True(t, handlerA.Completed(), "stream 10 must unblock upon RIC=1 insertion")
	assert.Equal(t, "alpha", handlerA.Headers()[0].Value)
	assert.False(t, handlerB.Completed(), "stream 20 must remain blocked")
	assert.False(t, handlerC.Completed(), "stream 30 must remain blocked")

	// Instruction 2: inserts "x-dep-2" -> InsertedEntryCount reaches 2
	decoder.InsertWithoutNameReference("x-dep-2", "beta")
	// Stream 20 (RIC=2) must unblock immediately!
	assert.True(t, handlerB.Completed(), "stream 20 must unblock upon RIC=2 insertion")
	assert.Equal(t, "beta", handlerB.Headers()[0].Value)
	assert.False(t, handlerC.Completed(), "stream 30 must remain blocked")

	// Instruction 3: inserts "x-dep-3" -> InsertedEntryCount reaches 3
	decoder.InsertWithoutNameReference("x-dep-3", "gamma")
	// Stream 30 (RIC=3) must unblock immediately!
	assert.True(t, handlerC.Completed(), "stream 30 must unblock upon RIC=3 insertion")
	assert.Equal(t, "gamma", handlerC.Headers()[0].Value)

	// Clean assertion: all handlers completed cleanly with zero errors
	assert.Equal(t, 1, len(handlerA.Headers()))
	assert.Equal(t, 1, len(handlerB.Headers()))
	assert.Equal(t, 1, len(handlerC.Headers()))
}

// TestQPACK_F11_LiteralFallback_WhenQuotaSaturated verifies that when maxBlockedStreams is 1,
// subsequent streams exceeding the quota fall back to literal header fields without data loss.
func TestQPACK_F11_LiteralFallback_WhenQuotaSaturated(t *testing.T) {
	t.Parallel()

	const maxBlocked = 1
	encSender := &stressSenderDelegate{}
	decSender := &stressSenderDelegate{}

	encoder := qpack.NewEncoderWithDefaults(nil)
	encoder.SetMaximumDynamicTableCapacity(4096)
	encoder.SetDynamicTableCapacity(4096)
	encoder.SetMaximumBlockedStreams(maxBlocked)
	encoder.SetStreamSenderDelegate(encSender)

	decoder := qpack.NewDecoder(4096, maxBlocked, nil)
	decoder.SetDynamicTableCapacity(4096)
	decoder.SetStreamSenderDelegate(decSender)

	// Stream 1 claims the single available blocked stream quota
	block1 := encoder.EncodeHeaderList(1, []qpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: "x-blocking-entry", Value: "blocks-the-line"},
	}, nil)
	require.NotEmpty(t, block1)
	assert.True(t, encoder.BlockingManager().IsBlocked(1))

	// Streams 2 to 20 attempt to encode novel headers: must ALL fall back to literal representations
	const totalFallbackStreams = 20
	blocks := make([][]byte, totalFallbackStreams)

	for i := 2; i <= totalFallbackStreams; i++ {
		streamID := uint64(i)
		headers := []qpack.HeaderField{
			{Name: ":method", Value: "POST"},
			{Name: fmt.Sprintf("x-fallback-k-%d", i), Value: fmt.Sprintf("fallback-v-%d", i)},
		}
		block := encoder.EncodeHeaderList(streamID, headers, nil)
		require.NotEmpty(t, block)
		assert.False(t, encoder.BlockingManager().IsBlocked(streamID))
		assert.Equal(t, byte(0x00), block[0], "fallback stream must declare RIC=0 prefix")
		blocks[i-1] = block
	}

	// Decode all fallback streams through the decoder: all must decode immediately (without blocking)
	for i := 2; i <= totalFallbackStreams; i++ {
		handler := &stressHeadersHandler{}
		pDec := decoder.CreateProgressiveDecoder(uint64(i), handler)
		pDec.Decode(blocks[i-1])
		pDec.EndHeaderBlock()

		assert.True(t, handler.Completed())
		assert.Equal(t, uint64(0), handler.ErrorCode())
		require.Equal(t, 2, len(handler.Headers()))
		assert.Equal(t, ":method", handler.Headers()[0].Name)
		assert.Equal(t, "POST", handler.Headers()[0].Value)
		assert.Equal(t, fmt.Sprintf("x-fallback-k-%d", i), handler.Headers()[1].Name)
		assert.Equal(t, fmt.Sprintf("fallback-v-%d", i), handler.Headers()[1].Value)
	}
}

// TestQPACK_F11_LiteralFallback_WhenDynamicTableFull tests that when dynamic table capacity
// is exhausted and existing unacknowledged entries cannot be evicted (eviction barrier active),
// the encoder falls back to literal representations rather than violating the barrier.
func TestQPACK_F11_LiteralFallback_WhenDynamicTableFull(t *testing.T) {
	t.Parallel()

	// Table capacity 100 bytes can hold at most two ~38 byte entries (32 byte overhead + name + val)
	const smallCapacity = 100
	encSender := &stressSenderDelegate{}
	decSender := &stressSenderDelegate{}

	encoder := qpack.NewEncoderWithDefaults(nil)
	encoder.SetMaximumDynamicTableCapacity(smallCapacity)
	encoder.SetDynamicTableCapacity(smallCapacity)
	encoder.SetMaximumBlockedStreams(10)
	encoder.SetStreamSenderDelegate(encSender)

	decoder := qpack.NewDecoder(smallCapacity, 10, nil)
	decoder.SetDynamicTableCapacity(smallCapacity)
	decoder.SetStreamSenderDelegate(decSender)

	// Stream 1 inserts entry A (size: 3 + 3 + 32 = 38 bytes)
	blockA := encoder.EncodeHeaderList(1, []qpack.HeaderField{{Name: "kA1", Value: "vA1"}}, nil)
	require.NotEmpty(t, blockA)

	// Stream 2 inserts entry B (size: 38 bytes, total 76 <= 100)
	blockB := encoder.EncodeHeaderList(2, []qpack.HeaderField{{Name: "kB1", Value: "vB1"}}, nil)
	require.NotEmpty(t, blockB)

	// Dynamic table now holds 76 bytes. Inserting a 38-byte entry C would require evicting entry A.
	// But entry A is referenced by stream 1 and has NOT been acknowledged yet (eviction barrier)!
	// Stream 3 attempts to insert entry C: encoder must refuse eviction and fall back to literal!
	blockC := encoder.EncodeHeaderList(3, []qpack.HeaderField{{Name: "kC1", Value: "vC1"}}, nil)
	require.NotEmpty(t, blockC)

	// Verify table size did NOT expand beyond smallCapacity and entry A was not evicted
	assert.Equal(t, uint64(0), encoder.HeaderTable().DroppedEntryCount())
	assert.Equal(t, uint64(2), encoder.HeaderTable().InsertedEntryCount())

	// Deliver encoder instructions to decoder
	decoder.EncoderStreamReceiver().Decode(encSender.Drain())

	// Decode all 3 blocks
	for i, blk := range [][]byte{blockA, blockB, blockC} {
		handler := &stressHeadersHandler{}
		pDec := decoder.CreateProgressiveDecoder(uint64(i+1), handler)
		pDec.Decode(blk)
		pDec.EndHeaderBlock()

		assert.True(t, handler.Completed())
		assert.Equal(t, uint64(0), handler.ErrorCode())
		assert.Equal(t, 1, len(handler.Headers()))
	}
}

// TestQPACK_F11_HighConcurrency_NoDeadlock_Soak stresses concurrent stream creation
// and header serialization across 40 goroutines executing 600 streams under tight
// blocked stream limits (maxBlockedStreams=5), verifying zero deadlocks and zero data races.
func TestQPACK_F11_HighConcurrency_NoDeadlock_Soak(t *testing.T) {
	t.Parallel()

	codec := coreh3.NewQPACKCodecWithOptions(8192, 5, nil)
	codec.Encoder().SetStreamSenderDelegate(&stressSenderDelegate{})

	const (
		numWorkers       = 40
		streamsPerWorker = 15
		totalStreams     = numWorkers * streamsPerWorker
	)

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	errCh := make(chan error, totalStreams)
	var completedCount atomic.Int64

	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			defer wg.Done()

			for s := 0; s < streamsPerWorker; s++ {
				streamID := uint64(workerID*1000 + s*2 + 1)

				head := headkit.NewWithCapacity(4)
				head.Set("content-type", "application/json")
				head.Set("x-worker", strconv.Itoa(workerID))
				head.Set("x-stream-seq", strconv.Itoa(s))
				head.Set(fmt.Sprintf("x-cust-%d", s%5), fmt.Sprintf("val-%d", workerID))

				block := codec.EncodeResponseHeaders(streamID, 200, head, 0)
				if len(block) == 0 {
					errCh <- fmt.Errorf("worker %d stream %d encoded block is empty", workerID, streamID)
					return
				}

				var respHeader mach.ResponseHeader
				statusCode, err := codec.DecodeResponseHeaders(streamID, block, &respHeader)
				if err != nil {
					errCh <- fmt.Errorf("worker %d stream %d decoding failed: %w", workerID, streamID, err)
					return
				}
				if statusCode != 200 {
					errCh <- fmt.Errorf("worker %d stream %d unexpected status: %d", workerID, streamID, statusCode)
					return
				}

				completedCount.Add(1)
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency soak failure: %v", err)
	}

	assert.Equal(t, int64(totalStreams), completedCount.Load())
	assert.Nil(t, codec.Err(), "QPACKCodec must have zero fatal errors")
}

// TestQPACK_F11_DecoderAdversarial_ExceedsBlockedStreamsQuota verifies that when an
// adversarial peer exceeds SETTINGS_QPACK_BLOCKED_STREAMS on the decoder, the decoder
// immediately rejects the violating stream with QPACK_DECOMPRESSION_FAILED and preserves
// healthy streams upon stream reset.
func TestQPACK_F11_DecoderAdversarial_ExceedsBlockedStreamsQuota(t *testing.T) {
	t.Parallel()

	const maxBlocked = 2
	decSender := &stressSenderDelegate{}
	decoder := qpack.NewDecoder(4096, maxBlocked, nil)
	decoder.SetDynamicTableCapacity(4096)
	decoder.SetStreamSenderDelegate(decSender)

	// Craft blocks with RIC > 0 using helper encoder
	encSender := &stressSenderDelegate{}
	encHelper := qpack.NewEncoderWithDefaults(nil)
	encHelper.SetMaximumDynamicTableCapacity(4096)
	encHelper.SetDynamicTableCapacity(4096)
	encHelper.SetMaximumBlockedStreams(10)
	encHelper.SetStreamSenderDelegate(encSender)

	blockA := encHelper.EncodeHeaderList(100, []qpack.HeaderField{{Name: "k-adv-1", Value: "v1"}}, nil)
	blockB := encHelper.EncodeHeaderList(200, []qpack.HeaderField{{Name: "k-adv-2", Value: "v2"}}, nil)
	blockC := encHelper.EncodeHeaderList(300, []qpack.HeaderField{{Name: "k-adv-3", Value: "v3"}}, nil)

	// Deliver block A (stream 100): blocks (count = 1)
	handlerA := &stressHeadersHandler{}
	pDecA := decoder.CreateProgressiveDecoder(100, handlerA)
	pDecA.Decode(blockA)
	pDecA.EndHeaderBlock()
	assert.False(t, handlerA.Completed())
	assert.Equal(t, uint64(0), handlerA.ErrorCode())

	// Deliver block B (stream 200): blocks (count = 2 == maxBlocked)
	handlerB := &stressHeadersHandler{}
	pDecB := decoder.CreateProgressiveDecoder(200, handlerB)
	pDecB.Decode(blockB)
	pDecB.EndHeaderBlock()
	assert.False(t, handlerB.Completed())
	assert.Equal(t, uint64(0), handlerB.ErrorCode())

	// Deliver block C (stream 300): EXCEEDS maxBlocked!
	handlerC := &stressHeadersHandler{}
	pDecC := decoder.CreateProgressiveDecoder(300, handlerC)
	pDecC.Decode(blockC)
	pDecC.EndHeaderBlock()

	// Verification: Decoder MUST reject stream 300 immediately
	assert.True(t, handlerC.ErrorCode() != 0, "decoder must flag error for exceeding quota")
	assert.Equal(t, uint64(qpack.QPACK_DECOMPRESSION_FAILED), handlerC.ErrorCode())
	assert.Contains(t, handlerC.ErrorMsg(), "Limit on number of blocked streams exceeded.")

	// Abort violating stream 300
	decoder.OnStreamReset(300)

	// Now deliver dynamic insertion to unblock stream 100
	decoder.InsertWithoutNameReference("k-adv-1", "v1")
	assert.True(t, handlerA.Completed(), "healthy stream 100 must unblock cleanly")
}

// =============================================================================
// Feature F12: Mid-Flight Stream Cancellation & Cleanup
// =============================================================================

// TestQPACK_F12_MidFlightStreamCancellation_BarrierRelease proves at the protocol level
// that when a stream referencing dynamic table entries is cancelled, its reference count
// is decremented, the eviction barrier is released, and subsequent large header insertions
// evict old entries rather than stalling or leaking.
func TestQPACK_F12_MidFlightStreamCancellation_BarrierRelease(t *testing.T) {
	t.Parallel()

	const maxTableCap = 512
	encoder := qpack.NewEncoderWithDefaults(nil)
	encoder.SetMaximumBlockedStreams(10)
	encoder.SetMaximumDynamicTableCapacity(maxTableCap)
	encoder.SetDynamicTableCapacity(maxTableCap)

	sender := &stressSenderDelegate{}
	encoder.SetStreamSenderDelegate(sender)

	// Stream 1 encodes unique headers that populate the dynamic table
	headers1 := []qpack.HeaderField{
		{Name: "x-custom-pin-header-1", Value: "val-pinned-1"},
		{Name: "x-custom-pin-header-2", Value: "val-pinned-2"},
	}
	_ = encoder.EncodeHeaderList(1, headers1, nil)

	bm := encoder.BlockingManager()
	smallestIdxBefore := bm.SmallestBlockingIndex()
	require.NotEqual(t, uint64(math.MaxUint64), smallestIdxBefore, "SmallestBlockingIndex must be pinned")

	// Stream 2 references the dynamic table entries inserted by Stream 1
	headers2 := []qpack.HeaderField{
		{Name: "x-custom-pin-header-1", Value: "val-pinned-1"},
	}
	_ = encoder.EncodeHeaderList(2, headers2, nil)

	// Cancel Stream 1
	encoder.OnStreamCancellation(1)
	require.False(t, bm.IsBlocked(1), "stream 1 must not be blocked")

	// SmallestBlockingIndex must still reflect Stream 2's pin
	require.NotEqual(t, uint64(math.MaxUint64), bm.SmallestBlockingIndex())

	// Cancel Stream 2
	encoder.OnStreamCancellation(2)
	require.False(t, bm.IsBlocked(2), "stream 2 must not be blocked")

	// Now all pins are gone: SmallestBlockingIndex must reset to math.MaxUint64
	assert.Equal(t, uint64(math.MaxUint64), bm.SmallestBlockingIndex(),
		"SmallestBlockingIndex must reset to MaxUint64 after all referencing streams are cancelled")

	// Now insert 20 new large unique headers that exceed 512 bytes capacity.
	// This forces FIFO eviction of the original entries.
	for i := 0; i < 20; i++ {
		largeHdr := []qpack.HeaderField{
			{Name: fmt.Sprintf("x-churn-header-%03d", i), Value: fmt.Sprintf("value-%03d-%s", i, strings.Repeat("x", 40))},
		}
		_ = encoder.EncodeHeaderList(uint64(100+i), largeHdr, nil)
		encoder.OnHeaderAcknowledgement(uint64(100 + i))
	}

	// Verify that dropped entry count has advanced (old entries evicted)
	assert.Greater(t, encoder.HeaderTable().DroppedEntryCount(), uint64(0),
		"Dynamic table must have evicted older unpinned entries without barrier stalls")
}

// TestQPACK_F12_StreamCancellation_Storm_ZeroLeaks runs a high-concurrency stress test
// with 120 concurrent streams on a shared encoder, where 50% are cancelled and 50%
// acknowledged, asserting 0 leaked entries and complete barrier reset.
func TestQPACK_F12_StreamCancellation_Storm_ZeroLeaks(t *testing.T) {
	t.Parallel()

	const (
		numStreams  = 120
		maxTableCap = 4096
		maxBlocked  = 100
	)

	encoder := qpack.NewEncoderWithDefaults(nil)
	encoder.SetMaximumBlockedStreams(maxBlocked)
	encoder.SetMaximumDynamicTableCapacity(maxTableCap)
	encoder.SetDynamicTableCapacity(maxTableCap)
	encoder.SetStreamSenderDelegate(&stressSenderDelegate{})

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numStreams; i++ {
		wg.Add(1)
		go func(streamID uint64) {
			defer wg.Done()

			headers := []qpack.HeaderField{
				{Name: fmt.Sprintf("x-storm-hdr-%d", streamID%15), Value: fmt.Sprintf("storm-val-%d", streamID)},
				{Name: ":status", Value: "200"},
			}

			mu.Lock()
			_ = encoder.EncodeHeaderList(streamID, headers, nil)
			mu.Unlock()

			// 50% cancelled, 50% acknowledged
			if streamID%2 == 0 {
				mu.Lock()
				encoder.OnStreamCancellation(streamID)
				mu.Unlock()
			} else {
				mu.Lock()
				encoder.OnHeaderAcknowledgement(streamID)
				mu.Unlock()
			}
		}(uint64(1000 + i*4))
	}

	wg.Wait()

	bm := encoder.BlockingManager()
	assert.Equal(t, uint64(0), bm.NumBlockedStreams(), "NumBlockedStreams must be 0")
	assert.Equal(t, uint64(math.MaxUint64), bm.SmallestBlockingIndex(), "SmallestBlockingIndex must be MaxUint64")
}

// TestH3_QPACK_F12_CancellationUnderHeavyChurn_IncomingStreamsMap exercises rapid
// stream cancellations during heavy header churn across multiplexed H3 streams,
// verifying zero panics in incoming streams map and zero data races.
func TestH3_QPACK_F12_CancellationUnderHeavyChurn_IncomingStreamsMap(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(12*time.Second))()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 20; i++ {
			w.Header().Set(fmt.Sprintf("x-server-dyn-%02d", i), fmt.Sprintf("val-%s-%d", r.URL.Path, i))
		}
		if r.URL.Path == "/slow" {
			time.Sleep(50 * time.Millisecond)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := testutil.NewH3Server(t, handler)
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		totalWorkers  = 40
		reqsPerWorker = 8
	)
	var wg sync.WaitGroup
	var cancelledCount atomic.Int64
	var successCount atomic.Int64

	for w := 0; w < totalWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				shouldCancel := (workerID+r)%2 == 0

				var ctx context.Context
				var cancel context.CancelFunc
				if shouldCancel {
					ctx, cancel = context.WithTimeout(context.Background(), time.Duration(1+(r%5))*time.Millisecond)
				} else {
					ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				}

				targetURL := server.URL() + "/fast"
				if shouldCancel {
					targetURL = server.URL() + "/slow"
				}

				req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
				if err != nil {
					cancel()
					continue
				}
				for h := 0; h < 15; h++ {
					req.Header.Set(fmt.Sprintf("x-client-churn-%02d", h), fmt.Sprintf("val-%d-%d-%d", workerID, r, h))
				}

				resp, doErr := client.HTTP().Do(req)
				cancel()

				if doErr != nil {
					cancelledCount.Add(1)
				} else {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						successCount.Add(1)
					}
				}
			}
		}(w)
	}

	wg.Wait()
	client.CloseIdleConnections()

	assert.Greater(t, successCount.Load(), int64(0), "Healthy requests must succeed")
	assert.Greater(t, cancelledCount.Load(), int64(0), "Cancelled requests must be recorded")
}

// TestAoni_H3_QPACK_F12_ContextCancellation_LeakCheck exercises full client context
// cancellations with rapid aborts, verifying that zero goroutines leak.
func TestAoni_H3_QPACK_F12_ContextCancellation_LeakCheck(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(12*time.Second), testutil.WithTolerance(0))()

	server := testutil.NewH3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("delayed-response"))
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const concurrentCancellations = 25
	var wg sync.WaitGroup

	for i := 0; i < concurrentCancellations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL()+"/delayed", nil)
			if err != nil {
				return
			}
			for h := 0; h < 20; h++ {
				req.Header.Set(fmt.Sprintf("x-cancel-dyn-%02d", h), fmt.Sprintf("data-%d-%d", id, h))
			}

			resp, doErr := client.HTTP().Do(req)
			if doErr == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			} else {
				assert.True(t, errors.Is(doErr, context.DeadlineExceeded) ||
					strings.Contains(doErr.Error(), "canceled") ||
					strings.Contains(doErr.Error(), "cancelled") ||
					strings.Contains(doErr.Error(), "deadline"),
					fmt.Sprintf("expected context cancellation error, got: %v", doErr))
			}
		}(i)
	}

	wg.Wait()
	client.CloseIdleConnections()
}

// =============================================================================
// End-to-End HTTP/3 Integration Tests (aoni.Client + testutil.NewH3Server)
// =============================================================================

// TestQPACK_H3_E2E_100UniqueHeaders_VaryingWorkload dispatches requests with 100 unique
// headers through aoni.Client to testutil.NewH3Server. Server verifies all 100 headers
// and returns 100 unique response headers, validating complete roundtrip fidelity and zero leaks.
func TestQPACK_H3_E2E_100UniqueHeaders_VaryingWorkload(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(12*time.Second))()

	const numHeaders = 100

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < numHeaders; i++ {
			expectedKey := fmt.Sprintf("X-Req-Header-%03d", i)
			expectedVal := fmt.Sprintf("req-val-%03d", i)
			actualVal := r.Header.Get(expectedKey)
			if actualVal != expectedVal {
				http.Error(w, fmt.Sprintf("mismatch for %s: got %q, want %q", expectedKey, actualVal, expectedVal), http.StatusBadRequest)
				return
			}
		}

		for i := 0; i < numHeaders; i++ {
			w.Header().Set(fmt.Sprintf("X-Resp-Header-%03d", i), fmt.Sprintf("resp-val-%03d", i))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("100-headers-ok"))
	})

	server := testutil.NewH3Server(t, handler)
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	for reqIdx := 0; reqIdx < 10; reqIdx++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/headers/%d", server.URL(), reqIdx), nil)
		require.NoError(t, err)

		for i := 0; i < numHeaders; i++ {
			req.Header.Set(fmt.Sprintf("X-Req-Header-%03d", i), fmt.Sprintf("req-val-%03d", i))
		}

		resp, err := client.HTTP().Do(req)
		cancel()
		require.NoError(t, err)

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "100-headers-ok", string(body))

		for i := 0; i < numHeaders; i++ {
			respKey := fmt.Sprintf("X-Resp-Header-%03d", i)
			expectedVal := fmt.Sprintf("resp-val-%03d", i)
			assert.Equal(t, expectedVal, resp.Header.Get(respKey))
		}
	}

	client.CloseIdleConnections()
}

// TestQPACK_H3_E2E_HeavyHeaderTurnover_ConcurrentSoak executes 25 parallel workers
// executing 20 requests each (500 total requests) with unique header sets per request,
// stressing QPACK frame serialization, buffer reuse, and concurrent multiplexing over HTTP/3.
func TestQPACK_H3_E2E_HeavyHeaderTurnover_ConcurrentSoak(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(15*time.Second))()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workerID := r.Header.Get("X-Worker-ID")
		reqID := r.Header.Get("X-Req-ID")
		w.Header().Set("X-Echo-Worker", workerID)
		w.Header().Set("X-Echo-Req", reqID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("soak-ok"))
	})

	server := testutil.NewH3Server(t, handler)
	defer server.Close()

	client := aoni.NewClient(nil, option.WithH3(aoni.WithH3TLSConfig(server.TLSConfig())))
	defer client.Close()

	const (
		numWorkers    = 25
		reqsPerWorker = 20
		totalRequests = numWorkers * reqsPerWorker
	)

	var wg sync.WaitGroup
	errCh := make(chan error, totalRequests)
	var completedCount atomic.Int64

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < reqsPerWorker; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet,
					fmt.Sprintf("%s/turnover/w%d/r%d", server.URL(), workerID, r), nil)
				if err != nil {
					cancel()
					errCh <- err
					return
				}

				req.Header.Set("X-Worker-ID", strconv.Itoa(workerID))
				req.Header.Set("X-Req-ID", strconv.Itoa(r))
				for h := 0; h < 10; h++ {
					req.Header.Set(fmt.Sprintf("X-Dynamic-%d", h), fmt.Sprintf("val-%d-%d-%d", workerID, r, h))
				}

				resp, err := client.HTTP().Do(req)
				if err != nil {
					cancel()
					errCh <- fmt.Errorf("worker %d req %d Do failed: %w", workerID, r, err)
					return
				}

				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()
				if err != nil {
					errCh <- fmt.Errorf("worker %d req %d read body failed: %w", workerID, r, err)
					return
				}

				if resp.StatusCode != http.StatusOK || string(body) != "soak-ok" {
					errCh <- fmt.Errorf("worker %d req %d bad response: code %d, body %s", workerID, r, resp.StatusCode, body)
					return
				}

				completedCount.Add(1)
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Heavy header turnover soak error: %v", err)
	}

	assert.Equal(t, int64(totalRequests), completedCount.Load())
	client.CloseIdleConnections()
}

// TestQPACK_F11_H3_E2E_ZeroBlockedStreams_Concurrency runs a high-concurrency end-to-end
// test through aoni.Client with option.WithH3() where SETTINGS_QPACK_BLOCKED_STREAMS is set
// to 0. It verifies that client requests complete with 200 OK without deadlocks, dropped
// streams, or goroutine leaks.
func TestQPACK_F11_H3_E2E_ZeroBlockedStreams_Concurrency(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(12*time.Second))()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Echo-Proto", r.Proto)
		w.Header().Set("X-Echo-Req", r.Header.Get("X-Req-ID"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("zero-blocked-ok"))
	})

	server := testutil.NewH3Server(t, handler)
	defer server.Close()

	settings := &coreh3.Settings{
		MaxFieldSectionSize: 64 * 1024,
		QpackMaxTableCap:    4096,
		QpackBlockedStreams: 0,
		EnableDatagrams:     true,
	}

	client := aoni.NewClient(nil, option.WithH3(
		aoni.WithH3Settings(settings),
		aoni.WithH3TLSConfig(server.TLSConfig()),
	))
	defer client.Close()

	const (
		numGoroutines   = 20
		reqsPerRoutine  = 10
		totalH3Requests = numGoroutines * reqsPerRoutine
	)

	var wg sync.WaitGroup
	errCh := make(chan error, totalH3Requests)
	var completedCount atomic.Int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for r := 0; r < reqsPerRoutine; r++ {
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				req, err := http.NewRequestWithContext(
					ctx,
					http.MethodGet,
					fmt.Sprintf("%s/zero-blocked/%d/%d", server.URL(), routineID, r),
					nil,
				)
				if err != nil {
					cancel()
					errCh <- err
					return
				}

				req.Header.Set("X-Req-ID", fmt.Sprintf("id-%d-%d", routineID, r))
				req.Header.Set(fmt.Sprintf("X-Unique-%d", r), fmt.Sprintf("val-%d-%d", routineID, r))

				resp, err := client.HTTP().Do(req)
				if err != nil {
					cancel()
					errCh <- fmt.Errorf("routine %d req %d failed: %w", routineID, r, err)
					return
				}

				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()

				if err != nil {
					errCh <- fmt.Errorf("routine %d req %d read body failed: %w", routineID, r, err)
					return
				}

				if resp.StatusCode != http.StatusOK || string(body) != "zero-blocked-ok" {
					errCh <- fmt.Errorf("unexpected resp: code %d, body %s", resp.StatusCode, body)
					return
				}

				completedCount.Add(1)
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("zero-blocked H3 E2E failure: %v", err)
	}

	assert.Equal(t, int64(totalH3Requests), completedCount.Load())
	client.CloseIdleConnections()
}

// TestQPACK_F11_H3_E2E_ConstrainedBlockedStreams_Soak runs an intensive multi-stream soak
// test over aoni.Client with HTTP/3 under a tight blocked streams limit (QpackBlockedStreams=5),
// generating high churn headers across 25 concurrent workers (500 total requests) with zero leaks.
func TestQPACK_F11_H3_E2E_ConstrainedBlockedStreams_Soak(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(15*time.Second))()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Echo-Worker", r.Header.Get("X-Worker"))
		w.Header().Set("X-Echo-Seq", r.Header.Get("X-Seq"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("soak-pass"))
	})

	server := testutil.NewH3Server(t, handler)
	defer server.Close()

	settings := &coreh3.Settings{
		MaxFieldSectionSize: 128 * 1024,
		QpackMaxTableCap:    4096,
		QpackBlockedStreams: 5,
		EnableDatagrams:     true,
	}

	client := aoni.NewClient(nil, option.WithH3(
		aoni.WithH3Settings(settings),
		aoni.WithH3TLSConfig(server.TLSConfig()),
	))
	defer client.Close()

	const (
		numWorkers    = 25
		reqsPerWorker = 20
		totalSoakReqs = numWorkers * reqsPerWorker
	)

	var wg sync.WaitGroup
	errCh := make(chan error, totalSoakReqs)
	var completedCount atomic.Int64

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for s := 0; s < reqsPerWorker; s++ {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				req, err := http.NewRequestWithContext(
					ctx,
					http.MethodPost,
					fmt.Sprintf("%s/soak/w%d/s%d", server.URL(), workerID, s),
					bytes.NewReader([]byte("payload-data")),
				)
				if err != nil {
					cancel()
					errCh <- err
					return
				}

				req.Header.Set("X-Worker", strconv.Itoa(workerID))
				req.Header.Set("X-Seq", strconv.Itoa(s))
				req.Header.Set("X-Heavy-1", fmt.Sprintf("churn-val-%d-%d", workerID, s))
				req.Header.Set("X-Heavy-2", fmt.Sprintf("churn-hash-%x", workerID*31+s*17))

				resp, err := client.HTTP().Do(req)
				if err != nil {
					cancel()
					errCh <- fmt.Errorf("worker %d req %d failed: %w", workerID, s, err)
					return
				}

				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				cancel()

				if err != nil {
					errCh <- fmt.Errorf("worker %d req %d read body err: %w", workerID, s, err)
					return
				}

				if resp.StatusCode != http.StatusOK || string(body) != "soak-pass" {
					errCh <- fmt.Errorf("bad response: code %d, body %s", resp.StatusCode, body)
					return
				}

				completedCount.Add(1)
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("constrained blocked streams soak error: %v", err)
	}

	assert.Equal(t, int64(totalSoakReqs), completedCount.Load())
	client.CloseIdleConnections()
}
