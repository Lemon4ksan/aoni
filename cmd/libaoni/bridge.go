// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"sync"
	"time"
	"unsafe"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/option"
)

// Constants matching include/aoni.h error and profile definitions.
const (
	AONIOk                int32 = 0
	AONIErrNetwork        int32 = -1
	AONIErrBufferOverflow int32 = -2
	AONIErrTimeout        int32 = -3
	AONIErrInvalidParam   int32 = -4
	AONIErrClientNil      int32 = -5

	AONIBrowserNone    uint8 = 0
	AONIBrowserChrome  uint8 = 1
	AONIBrowserFirefox uint8 = 2
	AONIBrowserSafari  uint8 = 3

	maxSafeCStringLen = 4096
	maxBatchWorkers   = 8192
)

// Task represents the memory layout of C aoni_task_t in pure Go.
type Task struct {
	TaskID         uint64
	Method         *byte
	MethodLen      uintptr
	URL            *byte
	URLLen         uintptr
	HeadersRaw     *byte
	HeadersLen     uintptr
	BodyPtr        *byte
	BodyLen        uintptr
	RespBufPtr     *byte
	RespBufCap     uintptr
	RespBufLen     uintptr
	RespHeadersPtr *byte
	RespHeadersCap uintptr
	RespHeadersLen uintptr
	StatusCode     int32
	ErrorCode      int32
}

// Config represents the memory layout of C aoni_config_t in pure Go.
type Config struct {
	MaxConnsPerHost uint32
	Concurrency     uint32
	TimeoutMS       uint32
	BrowserProfile  uint8
	EnableHTTP2     uint8
	EnableHTTP3     uint8
	_               uint8 // alignment padding
	ProxyURL        *byte
}

// Version returns the version string of the library.
const Version = "1.0.0-silicon"

// NewClientFromConfig instantiates a fast.Client from the given Config.
func NewClientFromConfig(cfg *Config) *fast.Client {
	var opts []aoni.ClientOption

	if cfg != nil {
		if cfg.TimeoutMS > 0 {
			opts = append(opts, option.WithTimeout(time.Duration(cfg.TimeoutMS)*time.Millisecond))
		}

		switch cfg.BrowserProfile {
		case AONIBrowserChrome:
			opts = append(opts, option.WithChrome())
		case AONIBrowserFirefox:
			opts = append(opts, option.WithFirefox())
		case AONIBrowserSafari:
			opts = append(opts, option.WithSafari())
		}

		if cfg.ProxyURL != nil {
			proxyStr := bytePtrToString(cfg.ProxyURL)
			if proxyStr != "" {
				opts = append(opts, option.WithProxyString(proxyStr))
			}
		}
	}

	return fast.NewClient(opts...)
}

// DoTask executes a single Task on the client with zero heap allocations.
func DoTask(client *fast.Client, t *Task) int32 {
	if client == nil {
		if t != nil {
			t.ErrorCode = AONIErrClientNil
		}
		return AONIErrClientNil
	}
	if t == nil {
		return AONIErrInvalidParam
	}

	req := fast.NewRequest(nil)
	defer req.Release()

	// 1. Method
	if t.Method != nil && t.MethodLen > 0 {
		mBytes := unsafe.Slice(t.Method, int(t.MethodLen))
		req.SetMethodBytes(mBytes)
	} else {
		req.SetMethod("GET")
	}

	// 2. URL
	if t.URL != nil && t.URLLen > 0 {
		uBytes := unsafe.Slice(t.URL, int(t.URLLen))
		req.SetURIBytes(uBytes)
	}

	// 3. Raw Headers
	if t.HeadersRaw != nil && t.HeadersLen > 0 {
		hBytes := unsafe.Slice(t.HeadersRaw, int(t.HeadersLen))
		parseRawHeaders(hBytes, req)
	}

	// 4. Body
	if t.BodyPtr != nil && t.BodyLen > 0 {
		bBytes := unsafe.Slice(t.BodyPtr, int(t.BodyLen))
		req.SetBodyBytes(bBytes)
	}

	// 5. Execute
	resp, err := client.Do(req)
	if err != nil {
		t.StatusCode = 0
		t.ErrorCode = AONIErrNetwork
		return AONIErrNetwork
	}
	defer resp.Close()

	// 6. Write response headers to C buffer (if requested)
	if t.RespHeadersPtr != nil && t.RespHeadersCap > 0 {
		var rawHeaders []byte
		if fastResp, ok := resp.(*fast.Response); ok && fastResp != nil {
			rawHeaders = fastResp.RawHeaders()
		} else if h1Resp, ok := resp.EngineResponse().(*h1engine.Response); ok && h1Resp != nil {
			rawHeaders = h1Resp.Header.Header()
		}

		if len(rawHeaders) > 0 {
			hBuf := unsafe.Slice(t.RespHeadersPtr, int(t.RespHeadersCap))
			n := copy(hBuf, rawHeaders)
			t.RespHeadersLen = uintptr(n)

			if len(rawHeaders) > int(t.RespHeadersCap) {
				t.ErrorCode = AONIErrBufferOverflow
			}
		}
	}

	// 7. Write response body to C buffer
	t.StatusCode = int32(resp.StatusCode())
	body := resp.UnsafeBodyBytes()

	if t.RespBufPtr != nil && t.RespBufCap > 0 {
		cBuf := unsafe.Slice(t.RespBufPtr, int(t.RespBufCap))
		n := copy(cBuf, body)
		t.RespBufLen = uintptr(n)

		if len(body) > int(t.RespBufCap) {
			t.ErrorCode = AONIErrBufferOverflow
			return AONIErrBufferOverflow
		}
	} else {
		t.RespBufLen = uintptr(len(body))
	}

	if t.ErrorCode == 0 {
		t.ErrorCode = AONIOk
	}
	return t.StatusCode
}

// DoBatchTasks executes N tasks in parallel across Go's Netpoller using bounded worker pool concurrency.
func DoBatchTasks(client *fast.Client, tasks []Task) {
	if client == nil || len(tasks) == 0 {
		return
	}

	numWorkers := len(tasks)
	if numWorkers > maxBatchWorkers {
		numWorkers = maxBatchWorkers
	}

	taskChan := make(chan *Task, numWorkers*2)
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func() {
			defer wg.Done()
			for t := range taskChan {
				DoTask(client, t)
			}
		}()
	}

	for i := range tasks {
		taskChan <- &tasks[i]
	}
	close(taskChan)

	wg.Wait()
}

func parseRawHeaders(data []byte, req *fast.Request) {
	for len(data) > 0 {
		lineEnd := bytes.IndexByte(data, '\n')
		var line []byte
		if lineEnd >= 0 {
			line = data[:lineEnd]
			data = data[lineEnd+1:]
		} else {
			line = data
			data = nil
		}

		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		if len(line) == 0 {
			continue
		}

		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx <= 0 {
			continue
		}

		key := bytes.TrimSpace(line[:colonIdx])
		val := bytes.TrimSpace(line[colonIdx+1:])
		if len(key) > 0 {
			req.SetHeaderBytes(key, val)
		}
	}
}

func bytePtrToString(ptr *byte) string {
	if ptr == nil {
		return ""
	}
	var length int
	for p := ptr; *p != 0 && length < maxSafeCStringLen; p = (*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + 1)) {
		length++
	}
	if length == 0 {
		return ""
	}
	return string(unsafe.Slice(ptr, length))
}
