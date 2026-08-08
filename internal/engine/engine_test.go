// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine_test

import (
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/engine"
)

type mockEvictor struct {
	evicted atomic.Int32
}

func (m *mockEvictor) CloseIdleConnections() {
	m.evicted.Add(1)
}

func TestEngine_FullOrchestrator(t *testing.T) {
	t.Parallel()

	baseURL, err := url.Parse("https://api.example.com/v1/")
	require.NoError(t, err)

	headers := http.Header{
		"User-Agent":         []string{"Aoni/2.0"},
		"Sec-CH-UA-Mobile":   []string{"?0"},
		"Sec-CH-UA-Platform": []string{"\"Windows\""},
	}
	evictor := &mockEvictor{}

	eng := engine.NewEngine(baseURL, headers, evictor, 50*time.Millisecond, 2)
	defer eng.Close()

	// 1. PreparedConfig checks (Mutex-Free)
	assert.Equal(t, "https://api.example.com/v1", eng.Prepared.BaseURLTrimmedString)
	assert.Equal(t, "api.example.com", eng.Prepared.DefaultHostHeader)
	assert.Contains(t, string(eng.Prepared.PrecomputedClientHints), "Sec-CH-UA-Platform")

	// 2. AltSvc & Dispatcher checks
	eng.AltSvc.ParseAndStore("api.example.com", `h3=":443"; ma=86400`)
	assert.True(t, eng.AltSvc.HasH3Support("api.example.com"))
	proto := eng.Dispatcher.ResolveProtocol(baseURL, "")
	assert.Equal(t, engine.ProtocolHTTP3, proto)

	// 3. Referer Automaton checks
	targetURL, _ := url.Parse("https://other.com/login")

	eng.Referer.UpdateLastURL(baseURL)
	ref := eng.Referer.ComputeReferer(targetURL)
	assert.Equal(t, "https://api.example.com/", ref) // StrictOriginWhenCrossOrigin

	// 4. BufferPool checks
	buf := eng.BufferPool.Get()
	buf.WriteString("hello pool")
	assert.Equal(t, "hello pool", buf.String())
	eng.BufferPool.Put(buf)

	// 5. ExecutionCycle & Janitor checks
	cycle, ctx := eng.NewCycle(t.Context(), 5*time.Second, 3, 5)
	defer cycle.Finish()

	assert.True(t, cycle.NextAttempt())
	assert.True(t, eng.Janitor.Acquire(ctx))
	eng.Janitor.Release()
}
