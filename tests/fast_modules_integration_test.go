// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/telemetry/inspector"
)

func TestFast_Integration_Telemetry(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Server-Telemetry", "active")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "telemetry payload ok")
	}))
	defer ts.Close()

	trafficInspector := inspector.NewTrafficInspector(":0")
	c := fast.NewClient(option.WithInspector(trafficInspector))

	resp, err := c.Request(context.Background(), "GET", ts.URL)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())

	records := trafficInspector.GetRequests()
	require.NotEmpty(t, records)
	assert.Equal(t, "GET", records[0].Method)
	assert.Equal(t, http.StatusOK, records[0].Status)
}

func TestFast_Integration_AntiDPIAndHeaderOrdering(t *testing.T) {
	var receivedHeaders []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k := range r.Header {
			receivedHeaders = append(receivedHeaders, k)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()

	resp, err := c.Request(
		context.Background(),
		"GET",
		ts.URL,
		mod.WithHeader("X-Anti-DPI", "bypassed"),
		mod.WithUserAgent("Aoni-AntiDPI-Agent/1.0"),
	)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.NotEmpty(t, receivedHeaders)
}

func TestFast_Integration_TLSEvasionAndBrowserProfiles(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "evasion ok")
	}))
	defer ts.Close()

	c := fast.NewClient(
		option.WithTLSFingerprint(aoni.BrowserChrome),
		option.WithInsecureSkipVerify(),
		option.WithTimeout(5*time.Second),
	)

	resp, err := c.Request(
		context.Background(),
		"GET",
		ts.URL,
		mod.WithHeader("Accept-Language", "en-US,en;q=0.9"),
	)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "evasion ok", string(resp.BodyBytes()))
}
