// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/tests/testutil"
)

func TestFast_MultiProtocol_H1_H2_H3(t *testing.T) {
	defer testutil.Check(t, testutil.WithTimeout(5*time.Second))()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Server-Proto", r.Proto)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fast-ok-" + r.Proto))
	})

	// 1. In-process HTTP/1.1 Server
	h1Server := testutil.NewH1Server(t, handler)
	defer h1Server.Close()

	// 2. In-process HTTP/2 Server
	h2Server := testutil.NewH2Server(t, handler)
	defer h2Server.Close()

	// 3. In-process HTTP/3 Server
	h3Server := testutil.NewH3Server(t, handler)
	defer h3Server.Close()

	// -------------------------------------------------------------------------
	// A. Test fast.Client over HTTP/1.1
	// -------------------------------------------------------------------------
	t.Run("FastClient_H1", func(t *testing.T) {
		fc := fast.NewClient()
		defer fc.Engine().CloseIdleConnections()

		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(h1Server.URL() + "/h1-ping")

		resp, err := fc.Do(req)
		req.Release()
		require.NoError(t, err)
		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		body := resp.BodyBytes()
		assert.Equal(t, "fast-ok-HTTP/1.1", string(body))
	})

	// -------------------------------------------------------------------------
	// B. Test fast.Client over HTTP/2 (Multiplexed)
	// -------------------------------------------------------------------------
	t.Run("FastClient_H2_Multiplexed", func(t *testing.T) {
		fc := fast.NewClient(fast.WithTLSConfig(h2Server.TLSConfig()), fast.WithH2())
		defer fc.Engine().CloseIdleConnections()

		var wg sync.WaitGroup
		const parallelReqs = 20
		errCh := make(chan error, parallelReqs)

		for i := 0; i < parallelReqs; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				req := fast.NewRequest(nil)
				req.SetContext(ctx)
				req.SetMethod(http.MethodGet)
				req.SetURL(h2Server.URL() + "/h2-multiplex")

				resp, err := fc.Do(req)
				req.Release()
				if err != nil {
					errCh <- err
					return
				}
				defer resp.Close()

				if resp.StatusCode() != http.StatusOK {
					errCh <- err
					return
				}
				if string(resp.BodyBytes()) != "fast-ok-HTTP/2.0" {
					errCh <- err
					return
				}
			}()
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("fast H2 request failed: %v", err)
		}
	})

	// -------------------------------------------------------------------------
	// C. Test fast.Client over HTTP/3 (QUIC Multiplexed)
	// -------------------------------------------------------------------------
	t.Run("FastClient_H3_QUIC", func(t *testing.T) {
		fc := fast.NewClient(fast.WithTLSConfig(h3Server.TLSConfig()), fast.WithH3())
		defer fc.Engine().CloseIdleConnections()

		var wg sync.WaitGroup
		const parallelH3Reqs = 15
		errCh := make(chan error, parallelH3Reqs)

		for i := 0; i < parallelH3Reqs; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()

				req := fast.NewRequest(nil)
				req.SetContext(ctx)
				req.SetMethod(http.MethodGet)
				req.SetURL(h3Server.URL() + "/h3-quic")

				resp, err := fc.Do(req)
				req.Release()
				if err != nil {
					errCh <- err
					return
				}
				defer resp.Close()

				if resp.StatusCode() != http.StatusOK {
					errCh <- err
					return
				}
				if string(resp.BodyBytes()) != "fast-ok-HTTP/3.0" {
					errCh <- err
					return
				}
			}()
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("fast H3 request failed: %v", err)
		}
	})
}
