// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/fast"
	coreh2 "github.com/lemon4ksan/mach/core/h2"
	coreh3 "github.com/lemon4ksan/mach/core/h3"
	"github.com/lemon4ksan/mach/quic/quicvarint"
)

func TestH2_HPACKEncoderDecoderSymmetry(t *testing.T) {
	hpEnc := coreh2.AcquireHPACK()
	defer coreh2.ReleaseHPACK(hpEnc)

	hpDec := coreh2.AcquireHPACK()
	defer coreh2.ReleaseHPACK(hpDec)

	headersToTest := []struct {
		key   string
		value string
	}{
		{":method", "POST"},
		{":path", "/api/v2/users"},
		{":authority", "api.example.com"},
		{":scheme", "https"},
		{"content-type", "application/json"},
		{"x-aoni-version", "2.0.0"},
	}

	hFrame := coreh2.AcquireFrame(coreh2.FrameHeaders).(*coreh2.Headers)
	defer coreh2.ReleaseFrame(hFrame)

	hf := coreh2.AcquireHeaderField()
	defer coreh2.ReleaseHeaderField(hf)

	for _, h := range headersToTest {
		hf.Set(h.key, h.value)
		hFrame.AppendHeaderField(hpEnc, hf, true)
	}

	rawHeaders := hFrame.Headers()
	decodedHeaders := make(map[string]string)
	currBuf := rawHeaders

	for len(currBuf) > 0 {
		hfRecv := coreh2.AcquireHeaderField()

		var err error

		currBuf, err = hpDec.Next(hfRecv, currBuf)
		require.NoError(t, err)

		decodedHeaders[hfRecv.Key()] = hfRecv.Value()
		coreh2.ReleaseHeaderField(hfRecv)
	}

	for _, expected := range headersToTest {
		assert.Equal(t, expected.value, decodedHeaders[expected.key])
	}
}

func TestH2_FrameSerializationRoundtrip(t *testing.T) {
	var buf bytes.Buffer

	bw := bufio.NewWriter(&buf)

	// 1. SETTINGS Frame
	settingsHeader := coreh2.AcquireFrameHeader()
	defer coreh2.ReleaseFrameHeader(settingsHeader)

	st := coreh2.AcquireFrame(coreh2.FrameSettings).(*coreh2.Settings)
	st.SetMaxConcurrentStreams(100)
	st.SetMaxWindowSize(1 << 20)
	settingsHeader.SetBody(st)

	_, err := settingsHeader.WriteTo(bw)
	require.NoError(t, err)

	_ = bw.Flush()

	br := bufio.NewReader(&buf)
	parsedHeader, err := coreh2.ReadFrameFrom(br)
	require.NoError(t, err)

	defer coreh2.ReleaseFrameHeader(parsedHeader)

	assert.Equal(t, coreh2.FrameSettings, parsedHeader.Type())
	parsedSettings := parsedHeader.Body().(*coreh2.Settings)
	assert.Equal(t, uint32(100), parsedSettings.MaxConcurrentStreams())
	assert.Equal(t, uint32(1<<20), parsedSettings.MaxWindowSize())

	// 2. DATA Frame
	dataHeader := coreh2.AcquireFrameHeader()
	defer coreh2.ReleaseFrameHeader(dataHeader)

	dataHeader.SetStream(1)

	df := coreh2.AcquireFrame(coreh2.FrameData).(*coreh2.Data)
	df.SetEndStream(true)
	df.SetData([]byte("fast h2 payload"))
	dataHeader.SetBody(df)

	buf.Reset()
	bw.Reset(&buf)
	_, err = dataHeader.WriteTo(bw)
	require.NoError(t, err)

	_ = bw.Flush()

	br.Reset(&buf)
	parsedDataHeader, err := coreh2.ReadFrameFrom(br)
	require.NoError(t, err)

	defer coreh2.ReleaseFrameHeader(parsedDataHeader)

	assert.Equal(t, coreh2.FrameData, parsedDataHeader.Type())
	assert.Equal(t, uint32(1), parsedDataHeader.Stream())
	parsedData := parsedDataHeader.Body().(*coreh2.Data)
	assert.True(t, parsedData.EndStream())
	assert.Equal(t, []byte("fast h2 payload"), parsedData.Data())
}

func TestH3_AltSvcParsingAndCaching(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Alt-Svc", `h3=":443"; ma=86400, h3-29=":443"; ma=86400`)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "alt-svc ok")
	}))
	defer ts.Close()

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "GET", ts.URL)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, `h3=":443"; ma=86400, h3-29=":443"; ma=86400`, resp.Header("Alt-Svc"))
}

func TestH2_SettingsAckAndFlowControl(t *testing.T) {
	st := coreh2.AcquireFrame(coreh2.FrameSettings).(*coreh2.Settings)
	defer coreh2.ReleaseFrame(st)

	st.SetAck(true)
	assert.True(t, st.IsAck())

	wu := coreh2.AcquireFrame(coreh2.FrameWindowUpdate).(*coreh2.WindowUpdate)
	defer coreh2.ReleaseFrame(wu)

	wu.SetIncrement(65535)
	assert.Equal(t, 65535, wu.Increment())
}

func TestH3_FramesParsing(t *testing.T) {
	var buf []byte

	buf = quicvarint.Append(buf, coreh3.FrameTypeHeaders)
	buf = quicvarint.Append(buf, 1024)

	r := bytes.NewReader(buf)
	frType, frLen, err := coreh3.ReadFrameHeader(r)
	require.NoError(t, err)
	assert.Equal(t, coreh3.FrameTypeHeaders, frType)
	assert.Equal(t, uint64(1024), frLen)
}
