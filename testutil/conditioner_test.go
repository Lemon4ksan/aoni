// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/testutil"
)

func TestConditioner_LatencyAndJitter(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()

	cond := testutil.NetworkCondition{
		Latency: 50 * time.Millisecond,
		Jitter:  10 * time.Millisecond,
	}

	conditioner := testutil.NewNetworkConditioner(cond)
	client := &http.Client{Transport: conditioner}

	start := time.Now()
	resp, err := client.Get(ts.URL)
	elapsed := time.Since(start)

	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond)
}

func TestConditioner_Offline(t *testing.T) {
	t.Parallel()

	conditioner := testutil.NewNetworkConditioner(testutil.ProfileBlackout())
	client := &http.Client{Transport: conditioner}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	_, err = client.Do(req)
	assert.ErrorIs(t, err, testutil.ErrNetworkOffline)
}

func TestConditioner_Throttling(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("a", 10*1024) // 10 KB

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer ts.Close()

	// Throttle to 50 KB/s => 10 KB should take ~200ms
	cond := testutil.NetworkCondition{
		BandwidthRate: 50 * 1024,
	}

	conditioner := testutil.NewNetworkConditioner(cond)
	client := &http.Client{Transport: conditioner}

	start := time.Now()
	resp, err := client.Get(ts.URL)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, len(payload), len(body))
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestConditioner_PacketLossAndReset(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()

	// 100% packet loss
	condLoss := testutil.NetworkCondition{PacketLoss: 1.0}
	cond1 := testutil.NewNetworkConditioner(condLoss)
	client1 := &http.Client{Transport: cond1}

	_, err := client1.Get(ts.URL)
	assert.ErrorIs(t, err, testutil.ErrSimulatedPacketLoss)

	// 100% reset rate
	condReset := testutil.NetworkCondition{ResetRate: 1.0}
	cond2 := testutil.NewNetworkConditioner(condReset)
	client2 := &http.Client{Transport: cond2}

	_, err = client2.Get(ts.URL)
	assert.ErrorIs(t, err, testutil.ErrSimulatedConnectionReset)
}

func TestConditioner_ContextCancellation(t *testing.T) {
	t.Parallel()

	cond := testutil.NetworkCondition{
		Latency: 500 * time.Millisecond,
	}
	conditioner := testutil.NewNetworkConditioner(cond)
	client := &http.Client{Transport: conditioner}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	_, err = client.Do(req)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestConditioner_SetCondition(t *testing.T) {
	t.Parallel()

	conditioner := testutil.NewNetworkConditioner(testutil.ProfileLossyWiFi())
	assert.Equal(t, 25*time.Millisecond, conditioner.Condition().Latency)

	conditioner.SetCondition(testutil.ProfileSlowSatellite())
	assert.Equal(t, 600*time.Millisecond, conditioner.Condition().Latency)
}
