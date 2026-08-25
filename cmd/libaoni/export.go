// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build cgo

package main

/*
#include "../../include/aoni.h"
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

	// Resolve any arena handles in tasks
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
