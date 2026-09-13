// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stackless

import (
	"runtime"
	"sync"
)

// NewFunc returns a stackless wrapper for the function f.
//
// Unlike f, the returned stackless wrapper doesn't use stack space
// on the goroutine that calls it. Instead, it dispatches the execution
// to a fixed pool of background worker goroutines.
//
// This is critical for high-throughput servers. In Go, deeply nested function calls
// (such as zlib/gzip compression) cause the calling goroutine's stack to grow (stack split),
// which permanently allocates memory. If 10,000 concurrent goroutines all trigger
// a stack growth, memory usage spikes massively. By farming out the heavy work to a fixed pool,
// the caller's stack remains small, saving memory.
//
// The stackless wrapper returns false if the call cannot be processed
// at the moment due to high load (channel buffer full).
func NewFunc(f func(ctx any)) func(ctx any) bool {
	if f == nil {
		// developer sanity-check
		panic("BUG: f cannot be nil")
	}

	funcWorkCh := make(chan *funcWork, runtime.GOMAXPROCS(-1)*2048)
	onceInit := func() {
		n := runtime.GOMAXPROCS(-1)
		for range n {
			go funcWorker(funcWorkCh, f)
		}
	}
	var once sync.Once

	return func(ctx any) bool {
		once.Do(onceInit)
		fw := getFuncWork()
		fw.ctx = ctx

		select {
		case funcWorkCh <- fw:
		default:
			putFuncWork(fw)
			return false
		}
		<-fw.done
		putFuncWork(fw)
		return true
	}
}

func funcWorker(funcWorkCh <-chan *funcWork, f func(ctx any)) {
	for fw := range funcWorkCh {
		f(fw.ctx)
		fw.done <- struct{}{}
	}
}

func getFuncWork() *funcWork {
	v := funcWorkPool.Get()
	if v == nil {
		v = &funcWork{
			done: make(chan struct{}, 1),
		}
	}
	return v.(*funcWork) //nolint:forcetypeassert
}

func putFuncWork(fw *funcWork) {
	fw.ctx = nil
	funcWorkPool.Put(fw)
}

var funcWorkPool sync.Pool

type funcWork struct {
	ctx  any
	done chan struct{}
}
