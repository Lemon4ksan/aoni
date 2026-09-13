// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/lemon4ksan/mach/client/h1"
)

type ManagedRequest struct {
	req      *h1.Request
	released atomic.Bool
}

func (m *ManagedRequest) Release() {
	if m.released.CompareAndSwap(false, true) {
		h1.ReleaseRequest(m.req)
		mReqPool.Put(m)
	}
}

type ManagedResponse struct {
	resp     *h1.Response
	released atomic.Bool
}

func (m *ManagedResponse) Release() {
	if m.released.CompareAndSwap(false, true) {
		h1.ReleaseResponse(m.resp)
		mRespPool.Put(m)
	}
}

// mReqPool caches ManagedRequest instances to minimize heap allocations.
// DANGER: Instances must be fully drained and dereferenced before returning.
var mReqPool = sync.Pool{
	New: func() any { return new(ManagedRequest) },
}

// mRespPool caches ManagedResponse instances to minimize heap allocations.
// DANGER: Instances must be fully drained and dereferenced before returning.
var mRespPool = sync.Pool{
	New: func() any { return new(ManagedResponse) },
}

type requestMapShard struct {
	sync.Mutex
	m map[unsafe.Pointer]*ManagedRequest
}

var mReqShards [64]requestMapShard

type responseMapShard struct {
	sync.Mutex
	m map[unsafe.Pointer]*ManagedResponse
}

var mRespShards [64]responseMapShard

func init() {
	for i := range mReqShards {
		mReqShards[i].m = make(map[unsafe.Pointer]*ManagedRequest, 256)
	}
	for i := range mRespShards {
		mRespShards[i].m = make(map[unsafe.Pointer]*ManagedResponse, 256)
	}
}

func WrapRequest(req *h1.Request) *ManagedRequest {
	mReq := mReqPool.Get().(*ManagedRequest)
	mReq.req = req
	mReq.released.Store(false)

	ptr := unsafe.Pointer(req)
	shardIdx := (uintptr(ptr) >> 4) & 63
	shard := &mReqShards[shardIdx]

	shard.Lock()
	shard.m[ptr] = mReq
	shard.Unlock()

	return mReq
}

func WrapResponse(resp *h1.Response) *ManagedResponse {
	mResp := mRespPool.Get().(*ManagedResponse)
	mResp.resp = resp
	mResp.released.Store(false)

	ptr := unsafe.Pointer(resp)
	shardIdx := (uintptr(ptr) >> 4) & 63
	shard := &mRespShards[shardIdx]

	shard.Lock()
	shard.m[ptr] = mResp
	shard.Unlock()

	return mResp
}

func ReleaseRequestSafe(req *h1.Request) {
	ptr := unsafe.Pointer(req)
	shardIdx := (uintptr(ptr) >> 4) & 63
	shard := &mReqShards[shardIdx]

	shard.Lock()
	mr, ok := shard.m[ptr]
	if ok {
		delete(shard.m, ptr)
	}
	shard.Unlock()

	if ok {
		mr.Release()
	} else {
		h1.ReleaseRequest(req)
	}
}

func ReleaseResponseSafe(resp *h1.Response) {
	ptr := unsafe.Pointer(resp)
	shardIdx := (uintptr(ptr) >> 4) & 63
	shard := &mRespShards[shardIdx]

	shard.Lock()

	mr, ok := shard.m[ptr]
	if ok {
		delete(shard.m, ptr)
	}

	shard.Unlock()

	if ok {
		mr.Release()
	} else {
		h1.ReleaseResponse(resp)
	}
}
