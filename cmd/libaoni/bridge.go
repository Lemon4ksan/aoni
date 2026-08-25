// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"io"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/lemon4ksan/foundation/silicon/clock"
	"github.com/lemon4ksan/foundation/silicon/offheap"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/realtime/ws"
)

// Constants matching include/aoni.h error and profile definitions.
const (
	AONIOk                int32 = 0
	AONIErrNetwork        int32 = -1
	AONIErrBufferOverflow int32 = -2
	AONIErrTimeout        int32 = -3
	AONIErrInvalidParam   int32 = -4
	AONIErrClientNil      int32 = -5
	AONIErrOutOfMemory    int32 = -6
	AONIErrStreamClosed   int32 = -7

	AONIBrowserNone    uint8 = 0
	AONIBrowserChrome  uint8 = 1
	AONIBrowserFirefox uint8 = 2
	AONIBrowserSafari  uint8 = 3

	maxSafeCStringLen = 4096
	maxBatchWorkers   = 8192
)

// Task represents the memory layout of C aoni_task_t in pure Go.
type Task struct {
	TaskID          uint64
	Method          *byte
	MethodLen       uintptr
	URL             *byte
	URLLen          uintptr
	HeadersRaw      *byte
	HeadersLen      uintptr
	BodyPtr         *byte
	BodyLen         uintptr
	RespBufPtr      *byte
	RespBufCap      uintptr
	RespBufLen      uintptr
	RespHeadersPtr  *byte
	RespHeadersCap  uintptr
	RespHeadersLen  uintptr
	StatusCode      int32
	ErrorCode       int32
	DNSTimeNS       uint64
	TLSTimeNS       uint64
	TTFBNS          uint64
	TotalTimeNS     uint64
	Arena           unsafe.Pointer
	_InternalHandle unsafe.Pointer
}

// Config represents the memory layout of C aoni_config_t in pure Go.
type Config struct {
	MaxConnsPerHost uint32
	Concurrency     uint32
	TimeoutMS       uint32
	BrowserProfile  uint8
	EnableHTTP2     uint8
	EnableHTTP3     uint8
	_Pad            uint8 // alignment padding
	ProxyURL        *byte
}

// StreamConfig represents the memory layout of C aoni_stream_config_t in pure Go.
type StreamConfig struct {
	StreamID    uint64
	URL         *byte
	URLLen      uintptr
	Method      *byte
	MethodLen   uintptr
	HeadersRaw  *byte
	HeadersLen  uintptr
	IsWebSocket uint8
	_Pad        [7]uint8 // alignment padding
}

// StreamHandler defines Go-level callbacks matching C function pointers.
type StreamHandler struct {
	OnOpen  func(streamID uint64, statusCode int32, userData unsafe.Pointer)
	OnData  func(streamID uint64, data []byte, isBinary int32, userData unsafe.Pointer)
	OnClose func(streamID uint64, code int32, reason string, userData unsafe.Pointer)
	OnError func(streamID uint64, errCode int32, msg string, userData unsafe.Pointer)
}

// StreamSession manages an active bidirectional stream (WebSocket, SSE, Streaming gRPC).
type StreamSession struct {
	StreamID uint64
	UserData unsafe.Pointer
	Handler  StreamHandler
	WSConn   ws.Conn
	Body     io.ReadCloser
	Cancel   context.CancelFunc
	Closed   atomic.Bool
}

// Version returns the version string of the library.
const Version = aoni.Version + "-silicon"

var arenaMutex sync.Mutex

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

// DoTask executes a single Task on the client with zero Go GC overhead.
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
	startNano := clock.CoarseNowNano()
	resp, err := client.Do(req)
	endNano := clock.CoarseNowNano()
	t.TotalTimeNS = uint64(endNano - startNano)
	t.TTFBNS = t.TotalTimeNS

	if err != nil {
		t.StatusCode = 0
		t.ErrorCode = AONIErrNetwork
		return AONIErrNetwork
	}
	defer resp.Close()

	// 6. Response Headers
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

	// 7. Response Body & Memory Dispatch
	t.StatusCode = int32(resp.StatusCode())
	body := resp.UnsafeBodyBytes()
	bodyLen := len(body)

	// Mode 1: Off-Heap Arena Allocation
	if t.Arena != nil {
		arena := (*offheap.Arena)(t.Arena)
		arenaMutex.Lock()
		ptr := arena.Alloc(bodyLen)
		arenaMutex.Unlock()

		if ptr == nil {
			t.ErrorCode = AONIErrOutOfMemory
			return AONIErrOutOfMemory
		}

		if bodyLen > 0 {
			dst := unsafe.Slice((*byte)(ptr), bodyLen)
			copy(dst, body)
		}
		t.RespBufPtr = (*byte)(ptr)
		t.RespBufCap = uintptr(bodyLen)
		t.RespBufLen = uintptr(bodyLen)

	} else if t.RespBufPtr != nil && t.RespBufCap > 0 {
		// Mode 2: Pre-allocated buffer by caller
		cBuf := unsafe.Slice(t.RespBufPtr, int(t.RespBufCap))
		n := copy(cBuf, body)
		t.RespBufLen = uintptr(n)

		if bodyLen > int(t.RespBufCap) {
			t.ErrorCode = AONIErrBufferOverflow
			return AONIErrBufferOverflow
		}

	} else {
		// Mode 3: Dynamic Off-Heap Auto-Allocation (0% Go GC overhead)
		if bodyLen == 0 {
			t.RespBufPtr = nil
			t.RespBufCap = 0
			t.RespBufLen = 0
		} else {
			offBuf, allocErr := offheap.NewBuffer(bodyLen)
			if allocErr != nil {
				t.ErrorCode = AONIErrOutOfMemory
				return AONIErrOutOfMemory
			}

			_, _ = offBuf.Write(body)
			bufSlice := offBuf.Bytes()

			t.RespBufPtr = &bufSlice[0]
			t.RespBufCap = uintptr(offBuf.Cap())
			t.RespBufLen = uintptr(offBuf.Len())
			handle := cgo.NewHandle(offBuf)
			t._InternalHandle = unsafe.Pointer(uintptr(handle))
		}
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

// FreeTaskOffHeap safely releases auto-allocated off-heap memory bound to the task.
func FreeTaskOffHeap(t *Task) {
	if t == nil || t._InternalHandle == nil {
		return
	}

	handle := cgo.Handle(uintptr(t._InternalHandle))
	if buf, ok := handle.Value().(*offheap.OffHeapBuffer); ok && buf != nil {
		buf.Release()
	}
	handle.Delete()

	t._InternalHandle = nil
	t.RespBufPtr = nil
	t.RespBufCap = 0
	t.RespBufLen = 0
}

// StartStream initiates an asynchronous full-duplex stream (WebSocket or HTTP SSE/gRPC chunk stream).
func StartStream(client *fast.Client, cfg *StreamConfig, handler StreamHandler, userData unsafe.Pointer) *StreamSession {
	if client == nil || cfg == nil || cfg.URL == nil || cfg.URLLen == 0 {
		if handler.OnError != nil {
			handler.OnError(0, AONIErrInvalidParam, "invalid stream configuration", userData)
		}
		return nil
	}

	urlBytes := unsafe.Slice(cfg.URL, int(cfg.URLLen))
	urlStr := string(urlBytes)

	ctx, cancel := context.WithCancel(context.Background())
	sess := &StreamSession{
		StreamID: cfg.StreamID,
		UserData: userData,
		Handler:  handler,
		Cancel:   cancel,
	}

	if cfg.IsWebSocket != 0 {
		// Mode A: Full-Duplex WebSocket
		go func() {
			wsConn, resp, err := ws.DialWebSocket(ctx, client, urlStr)
			if err != nil {
				sess.Closed.Store(true)
				if handler.OnError != nil {
					handler.OnError(cfg.StreamID, AONIErrNetwork, err.Error(), userData)
				}
				return
			}
			sess.WSConn = wsConn

			statusCode := int32(101)
			if resp != nil {
				statusCode = int32(resp.StatusCode)
			}
			if handler.OnOpen != nil {
				handler.OnOpen(cfg.StreamID, statusCode, userData)
			}

			for {
				msgType, data, readErr := wsConn.ReadMessage()
				if readErr != nil {
					if !sess.Closed.Swap(true) {
						if handler.OnClose != nil {
							handler.OnClose(cfg.StreamID, 1000, readErr.Error(), userData)
						}
					}
					return
				}

				isBinary := int32(0)
				if msgType == ws.OpcodeBinary {
					isBinary = 1
				}

				if handler.OnData != nil {
					handler.OnData(cfg.StreamID, data, isBinary, userData)
				}
			}
		}()
	} else {
		// Mode B: HTTP SSE / Chunked Stream / Streaming gRPC
		go func() {
			req := fast.NewRequest(nil)
			defer req.Release()

			if cfg.Method != nil && cfg.MethodLen > 0 {
				mBytes := unsafe.Slice(cfg.Method, int(cfg.MethodLen))
				req.SetMethodBytes(mBytes)
			} else {
				req.SetMethod("GET")
			}

			req.SetURIBytes(urlBytes)

			if cfg.HeadersRaw != nil && cfg.HeadersLen > 0 {
				hBytes := unsafe.Slice(cfg.HeadersRaw, int(cfg.HeadersLen))
				parseRawHeaders(hBytes, req)
			}

			resp, err := client.Do(req)
			if err != nil {
				sess.Closed.Store(true)
				if handler.OnError != nil {
					handler.OnError(cfg.StreamID, AONIErrNetwork, err.Error(), userData)
				}
				return
			}

			bodyStream := resp.BodyStream()
			if bodyStream == nil {
				sess.Closed.Store(true)
				if handler.OnError != nil {
					handler.OnError(cfg.StreamID, AONIErrNetwork, "no stream body available", userData)
				}
				return
			}
			sess.Body = bodyStream

			if handler.OnOpen != nil {
				handler.OnOpen(cfg.StreamID, int32(resp.StatusCode()), userData)
			}

			buf := make([]byte, 32*1024)
			for {
				n, readErr := bodyStream.Read(buf)
				if n > 0 && handler.OnData != nil {
					handler.OnData(cfg.StreamID, buf[:n], 0, userData)
				}
				if readErr != nil {
					if !sess.Closed.Swap(true) {
						if readErr == io.EOF {
							if handler.OnClose != nil {
								handler.OnClose(cfg.StreamID, 0, "EOF", userData)
							}
						} else if handler.OnError != nil {
							handler.OnError(cfg.StreamID, AONIErrNetwork, readErr.Error(), userData)
						}
					}
					_ = bodyStream.Close()
					return
				}
			}
		}()
	}

	return sess
}

// Send writes payload or WebSocket message to the active stream.
func (s *StreamSession) Send(data []byte, isBinary int32) int32 {
	if s == nil || s.Closed.Load() {
		return AONIErrStreamClosed
	}

	if s.WSConn != nil {
		opcode := ws.OpcodeText
		if isBinary != 0 {
			opcode = ws.OpcodeBinary
		}
		err := s.WSConn.WriteMessage(opcode, data)
		if err != nil {
			return AONIErrNetwork
		}
		return AONIOk
	}

	return AONIErrInvalidParam
}

// Close terminates stream and releases underlying connections.
func (s *StreamSession) Close(code int32, reason string) {
	if s == nil || s.Closed.Swap(true) {
		return
	}

	if s.Cancel != nil {
		s.Cancel()
	}

	if s.WSConn != nil {
		_ = s.WSConn.Close()
	}

	if s.Body != nil {
		_ = s.Body.Close()
	}

	if s.Handler.OnClose != nil {
		s.Handler.OnClose(s.StreamID, code, reason, s.UserData)
	}
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
