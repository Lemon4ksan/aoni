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
	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
	"github.com/lemon4ksan/aoni/internal/fast/h3engine"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func TestH2_HPACKEncoderDecoderSymmetry(t *testing.T) {
	hpEnc := h2engine.AcquireHPACK()
	defer h2engine.ReleaseHPACK(hpEnc)

	hpDec := h2engine.AcquireHPACK()
	defer h2engine.ReleaseHPACK(hpDec)

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

	hFrame := h2engine.AcquireFrame(h2engine.FrameHeaders).(*h2engine.Headers)
	defer h2engine.ReleaseFrame(hFrame)

	hf := h2engine.AcquireHeaderField()
	defer h2engine.ReleaseHeaderField(hf)

	for _, h := range headersToTest {
		hf.Set(h.key, h.value)
		hFrame.AppendHeaderField(hpEnc, hf, true)
	}

	rawHeaders := hFrame.Headers()
	decodedHeaders := make(map[string]string)
	currBuf := rawHeaders

	for len(currBuf) > 0 {
		hfRecv := h2engine.AcquireHeaderField()

		var err error

		currBuf, err = hpDec.Next(hfRecv, currBuf)
		require.NoError(t, err)

		decodedHeaders[hfRecv.Key()] = hfRecv.Value()
		h2engine.ReleaseHeaderField(hfRecv)
	}

	for _, expected := range headersToTest {
		assert.Equal(t, expected.value, decodedHeaders[expected.key])
	}
}

func TestH2_FrameSerializationRoundtrip(t *testing.T) {
	var buf bytes.Buffer

	bw := bufio.NewWriter(&buf)

	// 1. SETTINGS Frame
	settingsHeader := h2engine.AcquireFrameHeader()
	defer h2engine.ReleaseFrameHeader(settingsHeader)

	st := h2engine.AcquireFrame(h2engine.FrameSettings).(*h2engine.Settings)
	st.SetMaxConcurrentStreams(100)
	st.SetMaxWindowSize(1 << 20)
	settingsHeader.SetBody(st)

	_, err := settingsHeader.WriteTo(bw)
	require.NoError(t, err)

	_ = bw.Flush()

	br := bufio.NewReader(&buf)
	parsedHeader, err := h2engine.ReadFrameFrom(br)
	require.NoError(t, err)

	defer h2engine.ReleaseFrameHeader(parsedHeader)

	assert.Equal(t, h2engine.FrameSettings, parsedHeader.Type())
	parsedSettings := parsedHeader.Body().(*h2engine.Settings)
	assert.Equal(t, uint32(100), parsedSettings.MaxConcurrentStreams())
	assert.Equal(t, uint32(1<<20), parsedSettings.MaxWindowSize())

	// 2. DATA Frame
	dataHeader := h2engine.AcquireFrameHeader()
	defer h2engine.ReleaseFrameHeader(dataHeader)

	dataHeader.SetStream(1)

	df := h2engine.AcquireFrame(h2engine.FrameData).(*h2engine.Data)
	df.SetEndStream(true)
	df.SetData([]byte("fast h2 payload"))
	dataHeader.SetBody(df)

	buf.Reset()
	bw.Reset(&buf)
	_, err = dataHeader.WriteTo(bw)
	require.NoError(t, err)

	_ = bw.Flush()

	br.Reset(&buf)
	parsedDataHeader, err := h2engine.ReadFrameFrom(br)
	require.NoError(t, err)

	defer h2engine.ReleaseFrameHeader(parsedDataHeader)

	assert.Equal(t, h2engine.FrameData, parsedDataHeader.Type())
	assert.Equal(t, uint32(1), parsedDataHeader.Stream())
	parsedData := parsedDataHeader.Body().(*h2engine.Data)
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
	st := h2engine.AcquireFrame(h2engine.FrameSettings).(*h2engine.Settings)
	defer h2engine.ReleaseFrame(st)

	st.SetAck(true)
	assert.True(t, st.IsAck())

	wu := h2engine.AcquireFrame(h2engine.FrameWindowUpdate).(*h2engine.WindowUpdate)
	defer h2engine.ReleaseFrame(wu)

	wu.SetIncrement(65535)
	assert.Equal(t, 65535, wu.Increment())
}

func TestH3_FramesParsing(t *testing.T) {
	var buf []byte

	buf = quicvarint.Append(buf, h3engine.FrameTypeHeaders)
	buf = quicvarint.Append(buf, 1024)

	r := bytes.NewReader(buf)
	frType, frLen, err := h3engine.ReadFrameHeader(r)
	require.NoError(t, err)
	assert.Equal(t, h3engine.FrameTypeHeaders, frType)
	assert.Equal(t, uint64(1024), frLen)
}
