// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

type ManagedRequest struct {
	req      *h1engine.Request
	released atomic.Bool
}

func (m *ManagedRequest) Release() {
	if m.released.CompareAndSwap(false, true) {
		h1engine.ReleaseRequest(m.req)
		mReqPool.Put(m)
	}
}

type ManagedResponse struct {
	resp     *h1engine.Response
	released atomic.Bool
}

func (m *ManagedResponse) Release() {
	if m.released.CompareAndSwap(false, true) {
		h1engine.ReleaseResponse(m.resp)
		mRespPool.Put(m)
	}
}

var mReqPool = sync.Pool{
	New: func() any { return new(ManagedRequest) },
}

var mRespPool = sync.Pool{
	New: func() any { return new(ManagedResponse) },
}

type responseMapShard struct {
	sync.Mutex
	m map[unsafe.Pointer]*ManagedResponse
}

var mRespShards [64]responseMapShard

func init() {
	for i := range mRespShards {
		mRespShards[i].m = make(map[unsafe.Pointer]*ManagedResponse, 256)
	}
}

func WrapRequest(req *h1engine.Request) *ManagedRequest {
	mReq := mReqPool.Get().(*ManagedRequest)
	mReq.req = req
	mReq.released.Store(false)
	req.SetUserValue("mReq", mReq)

	return mReq
}

func WrapResponse(resp *h1engine.Response) *ManagedResponse {
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

func ReleaseRequestSafe(req *h1engine.Request) {
	if mr, ok := req.UserValue("mReq").(*ManagedRequest); ok {
		mr.Release()
	} else {
		h1engine.ReleaseRequest(req)
	}
}

func ReleaseResponseSafe(resp *h1engine.Response) {
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
		h1engine.ReleaseResponse(resp)
	}
}
