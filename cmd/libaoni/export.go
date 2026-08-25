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
	DoBatchTasks(client, taskList)
}

// aoni_version returns the library version string.
//
//export aoni_version
func aoni_version() *C.char {
	return versionCString
}
