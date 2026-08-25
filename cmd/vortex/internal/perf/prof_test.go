// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package perf

import (
	"bytes"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestParseBenchmarkOutput(t *testing.T) {
	rawOutput := `
goos: windows
goarch: amd64
pkg: github.com/lemon4ksan/g-man-tf2/pkg/services/mannco
cpu: 12th Gen Intel(R) Core(TM) i5-12400F
Benchmark_API_GetCart-12                    	447756538	         0.1339 ns/op	       0 B/op	       0 allocs/op
Benchmark_API_AddToCartDirect-12            	395885430	         0.1392 ns/op	       0 B/op	       0 allocs/op
Benchmark_API_GetDepositInfo-12             	  6800947	         7.506 ns/op	       0 B/op	       0 allocs/op
Benchmark_API_InitiatePayment-12            	   702691	        82.22 ns/op	      16 B/op	       1 allocs/op
PASS
`

	records := parseBenchmarkOutput(rawOutput)
	require.Len(t, records, 4)

	assert.Equal(t, "API", records[0].Service)
	assert.Equal(t, "GetCart", records[0].Method)
	assert.InDelta(t, 0.1339, records[0].NsPerOp, 0.001)
	assert.True(t, records[0].ZeroAlloc)
	assert.Equal(t, "✔ PASS", records[0].Status)

	assert.Equal(t, "InitiatePayment", records[3].Method)
	assert.Equal(t, int64(16), records[3].BytesPerOp)
	assert.Equal(t, int64(1), records[3].AllocsPerOp)
	assert.False(t, records[3].ZeroAlloc)
	assert.Equal(t, "⚠️ ALLOC", records[3].Status)

	report := buildProfileReport("test-workspace", records)
	require.NotNil(t, report)
	assert.InDelta(t, 75.0, report.ZeroAllocRate, 0.1)

	var buf bytes.Buffer

	err := renderTerminalReport(&buf, report)
	require.NoError(t, err)

	outStr := buf.String()
	assert.Contains(t, outStr, "EXECUTIVE PERFORMANCE SUMMARY")
	assert.Contains(t, outStr, "ENDPOINT LATENCY & ALLOCATION LEDGER")
	assert.Contains(t, outStr, "GetCart")
	assert.Contains(t, outStr, "LATENCY TAX DECOMPOSITION")
}
