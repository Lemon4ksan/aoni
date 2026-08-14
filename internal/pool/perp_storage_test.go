// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool_test

import (
	"sync"
	"testing"

	"github.com/lemon4ksan/aoni/internal/pool"
)

func TestPerPStorageBasic(t *testing.T) {
	storage := pool.NewPerPStorage(func() *[]byte {
		b := make([]byte, 1024)
		return &b
	})

	b1 := storage.Get()
	if b1 == nil || len(*b1) != 1024 {
		t.Fatalf("expected non-nil 1024 byte buffer")
	}

	storage.Put(b1)

	b2 := storage.Get()
	if b2 == nil {
		t.Fatalf("expected non-nil buffer from storage after Put")
	}
}

func TestPerPStorageParallel(t *testing.T) {
	storage := pool.NewPerPStorage(func() *[]byte {
		b := make([]byte, 512)
		return &b
	})

	var wg sync.WaitGroup

	workers := 64
	iters := 1000

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < iters; j++ {
				buf := storage.Get()
				if buf == nil {
					t.Errorf("got nil buffer")
					return
				}

				storage.Put(buf)
			}
		}()
	}

	wg.Wait()
}
