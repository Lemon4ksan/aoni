// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/netdial"
)

func TestMultiNICDialer_SuccessWithLoopbackIPs(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("HELLO MULTI NIC"))
	}))
	defer ts.Close()

	// Provide 127.0.0.1 and 127.0.0.2 as candidate local IPs
	cfg := netdial.MultiNICConfig{
		LocalAddrs: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("127.0.0.2"),
		},
		StaggerDelay: 10 * time.Millisecond,
		DialTimeout:  2 * time.Second,
	}

	dialer, err := netdial.NewMultiNICDialer(cfg)
	require.NoError(t, err)

	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Get(ts.URL)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMultiNICDialer_FallbackOnInvalidNIC(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("FALLBACK OK"))
	}))
	defer ts.Close()

	var fallback net.Dialer

	cfg := netdial.MultiNICConfig{
		Interfaces:     []string{"non_existent_interface_999"},
		FallbackDialer: &fallback,
	}

	dialer, err := netdial.NewMultiNICDialer(cfg)
	require.NoError(t, err)

	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Get(ts.URL)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMultiNICDialer_NoCandidatesError(t *testing.T) {
	t.Parallel()

	cfg := netdial.MultiNICConfig{
		Interfaces: []string{"invalid_nic_001"},
	}

	_, err := netdial.NewMultiNICDialer(cfg)
	assert.ErrorIs(t, err, netdial.ErrNoNICsAvailable)
}

func TestMultiNICDialer_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := netdial.MultiNICConfig{
		LocalAddrs: []net.IP{
			net.ParseIP("127.0.0.1"),
		},
		StaggerDelay: 10 * time.Millisecond,
	}

	dialer, err := netdial.NewMultiNICDialer(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	_, err = dialer.DialContext(ctx, "tcp", "127.0.0.1:9999")
	assert.Error(t, err)
}
