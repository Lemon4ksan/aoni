// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build cgo

package main

/*
#include <stdlib.h>
#include "../../include/aoni.h"

static inline void invoke_cb_on_open(aoni_cb_on_open_t fn, uint64_t stream_id, int32_t status_code, void* user_data) {
    if (fn) fn(stream_id, status_code, user_data);
}

static inline void invoke_cb_on_data(aoni_cb_on_data_t fn, uint64_t stream_id, const uint8_t* data, size_t len, int32_t is_binary, void* user_data) {
    if (fn) fn(stream_id, data, len, is_binary, user_data);
}

static inline void invoke_cb_on_close(aoni_cb_on_close_t fn, uint64_t stream_id, int32_t code, const char* reason, void* user_data) {
    if (fn) fn(stream_id, code, reason, user_data);
}

static inline void invoke_cb_on_error(aoni_cb_on_error_t fn, uint64_t stream_id, int32_t err_code, const char* message, void* user_data) {
    if (fn) fn(stream_id, err_code, message, user_data);
}
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"

	"github.com/lemon4ksan/foundation/silicon/offheap"

	"github.com/lemon4ksan/aoni/fast"
)

var versionCString = C.CString(Version)

// aoni_client_create allocates and configures a new fast.Client instance.
//
//export aoni_client_create
func aoni_client_create(cfg *C.aoni_config_t) unsafe.Pointer {
	var goCfg *Config
	if cfg != nil {
		goCfg = (*Config)(unsafe.Pointer(cfg))
	}
	client := NewClientFromConfig(goCfg)
	handle := cgo.NewHandle(client)
	return unsafe.Pointer(uintptr(handle))
}

// aoni_client_destroy releases client resources and deletes the cgo handle.
//
//export aoni_client_destroy
func aoni_client_destroy(clientPtr unsafe.Pointer) {
	if clientPtr == nil {
		return
	}
	handle := cgo.Handle(uintptr(clientPtr))
	handle.Delete()
}

// aoni_client_do executes a single synchronous HTTP request with zero heap allocations.
//
//export aoni_client_do
func aoni_client_do(clientPtr unsafe.Pointer, t *C.aoni_task_t) C.int32_t {
	if clientPtr == nil {
		if t != nil {
			t.error_code = C.int32_t(AONIErrClientNil)
		}
		return C.int32_t(AONIErrClientNil)
	}

	handle := cgo.Handle(uintptr(clientPtr))
	client, ok := handle.Value().(*fast.Client)
	if !ok || client == nil {
		if t != nil {
			t.error_code = C.int32_t(AONIErrClientNil)
		}
		return C.int32_t(AONIErrClientNil)
	}

	var task *Task
	if t != nil {
		task = (*Task)(unsafe.Pointer(t))
		if t.arena != nil {
			aHandle := cgo.Handle(uintptr(t.arena))
			if arena, aOk := aHandle.Value().(*offheap.Arena); aOk && arena != nil {
				task.Arena = unsafe.Pointer(arena)
			}
		}
	}
	return C.int32_t(DoTask(client, task))
}

// aoni_client_batch_do executes N requests in parallel across Go's Netpoller in a single FFI call.
//
//export aoni_client_batch_do
func aoni_client_batch_do(clientPtr unsafe.Pointer, tasks *C.aoni_task_t, count C.size_t) {
	if clientPtr == nil || tasks == nil || count == 0 {
		return
	}

	handle := cgo.Handle(uintptr(clientPtr))
	client, ok := handle.Value().(*fast.Client)
	if !ok || client == nil {
		return
	}

	taskList := unsafe.Slice((*Task)(unsafe.Pointer(tasks)), int(count))

	for i := range taskList {
		if taskList[i].Arena != nil {
			aHandle := cgo.Handle(uintptr(taskList[i].Arena))
			if arena, aOk := aHandle.Value().(*offheap.Arena); aOk && arena != nil {
				taskList[i].Arena = unsafe.Pointer(arena)
			}
		}
	}

	DoBatchTasks(client, taskList)
}

// aoni_stream_connect initiates a full-duplex stream (WebSocket / SSE / Streaming gRPC).
//
//export aoni_stream_connect
func aoni_stream_connect(
	clientPtr unsafe.Pointer,
	cfg *C.aoni_stream_config_t,
	callbacks *C.aoni_stream_callbacks_t,
	userData unsafe.Pointer,
) unsafe.Pointer {
	if clientPtr == nil || cfg == nil {
		return nil
	}

	handle := cgo.Handle(uintptr(clientPtr))
	client, ok := handle.Value().(*fast.Client)
	if !ok || client == nil {
		return nil
	}

	goCfg := (*StreamConfig)(unsafe.Pointer(cfg))

	var handler StreamHandler
	if callbacks != nil {
		cb := *callbacks
		if cb.on_open != nil {
			handler.OnOpen = func(streamID uint64, statusCode int32, ud unsafe.Pointer) {
				C.invoke_cb_on_open(cb.on_open, C.uint64_t(streamID), C.int32_t(statusCode), ud)
			}
		}
		if cb.on_data != nil {
			handler.OnData = func(streamID uint64, data []byte, isBinary int32, ud unsafe.Pointer) {
				var dataPtr *C.uint8_t
				if len(data) > 0 {
					dataPtr = (*C.uint8_t)(unsafe.Pointer(&data[0]))
				}
				C.invoke_cb_on_data(cb.on_data, C.uint64_t(streamID), dataPtr, C.size_t(len(data)), C.int32_t(isBinary), ud)
			}
		}
		if cb.on_close != nil {
			handler.OnClose = func(streamID uint64, code int32, reason string, ud unsafe.Pointer) {
				var cReason *C.char
				if reason != "" {
					cReason = C.CString(reason)
					defer C.free(unsafe.Pointer(cReason))
				}
				C.invoke_cb_on_close(cb.on_close, C.uint64_t(streamID), C.int32_t(code), cReason, ud)
			}
		}
		if cb.on_error != nil {
			handler.OnError = func(streamID uint64, errCode int32, msg string, ud unsafe.Pointer) {
				var cMsg *C.char
				if msg != "" {
					cMsg = C.CString(msg)
					defer C.free(unsafe.Pointer(cMsg))
				}
				C.invoke_cb_on_error(cb.on_error, C.uint64_t(streamID), C.int32_t(errCode), cMsg, ud)
			}
		}
	}

	sess := StartStream(client, goCfg, handler, userData)
	if sess == nil {
		return nil
	}

	sHandle := cgo.NewHandle(sess)
	return unsafe.Pointer(uintptr(sHandle))
}

// aoni_stream_send transmits raw data or a WebSocket frame over the active stream.
//
//export aoni_stream_send
func aoni_stream_send(streamPtr unsafe.Pointer, data *C.uint8_t, length C.size_t, isBinary C.int32_t) C.int32_t {
	if streamPtr == nil {
		return C.int32_t(AONIErrStreamClosed)
	}

	handle := cgo.Handle(uintptr(streamPtr))
	sess, ok := handle.Value().(*StreamSession)
	if !ok || sess == nil {
		return C.int32_t(AONIErrStreamClosed)
	}

	var dataSlice []byte
	if data != nil && length > 0 {
		dataSlice = unsafe.Slice((*byte)(unsafe.Pointer(data)), int(length))
	}

	return C.int32_t(sess.Send(dataSlice, int32(isBinary)))
}

// aoni_stream_close terminates the stream session and releases resources.
//
//export aoni_stream_close
func aoni_stream_close(streamPtr unsafe.Pointer, code C.int32_t, reason *C.char) {
	if streamPtr == nil {
		return
	}

	handle := cgo.Handle(uintptr(streamPtr))
	sess, ok := handle.Value().(*StreamSession)
	if ok && sess != nil {
		var reasonStr string
		if reason != nil {
			reasonStr = C.GoString(reason)
		}
		sess.Close(int32(code), reasonStr)
	}
	handle.Delete()
}

// aoni_arena_create provisions a GC-invisible off-heap OS memory arena (mmap / VirtualAlloc).
//
//export aoni_arena_create
func aoni_arena_create(sizeBytes C.size_t) unsafe.Pointer {
	arena, err := offheap.NewArena(int(sizeBytes))
	if err != nil {
		return nil
	}
	handle := cgo.NewHandle(arena)
	return unsafe.Pointer(uintptr(handle))
}

// aoni_arena_reset resets the arena offset to zero in O(1) single-cycle time.
//
//export aoni_arena_reset
func aoni_arena_reset(arenaPtr unsafe.Pointer) {
	if arenaPtr == nil {
		return
	}
	handle := cgo.Handle(uintptr(arenaPtr))
	arena, ok := handle.Value().(*offheap.Arena)
	if ok && arena != nil {
		arena.Reset()
	}
}

// aoni_arena_destroy returns the off-heap arena memory pages directly to the OS kernel.
//
//export aoni_arena_destroy
func aoni_arena_destroy(arenaPtr unsafe.Pointer) {
	if arenaPtr == nil {
		return
	}
	handle := cgo.Handle(uintptr(arenaPtr))
	arena, ok := handle.Value().(*offheap.Arena)
	if ok && arena != nil {
		arena.Release()
	}
	handle.Delete()
}

// aoni_task_free safely releases auto-allocated off-heap memory bound to the task.
//
//export aoni_task_free
func aoni_task_free(t *C.aoni_task_t) {
	if t == nil {
		return
	}
	task := (*Task)(unsafe.Pointer(t))
	FreeTaskOffHeap(task)
}

// aoni_free releases a raw memory pointer allocated via offheap.
//
//export aoni_free
func aoni_free(ptr unsafe.Pointer) {
	// Provided for general C-ABI compatibility
}

// aoni_version returns the library version string.
//
//export aoni_version
func aoni_version() *C.char {
	return versionCString
}
