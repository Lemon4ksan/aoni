// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package sysnet

import (
	"errors"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ErrRIONotSupported = errors.New("rio: Registered I/O extensions not supported on this OS")
	ErrRIOFailed       = errors.New("rio: RIORegisterBuffer kernel syscall failed")

	modkernel32       = syscall.NewLazyDLL("kernel32.dll")
	procVirtualLock   = modkernel32.NewProc("VirtualLock")
	procVirtualUnlock = modkernel32.NewProc("VirtualUnlock")

	rioAvailable       atomic.Bool
	rioRegisterBufFn   uintptr
	rioDeregisterBufFn uintptr
)

type RIO_EXTENSION_FUNCTION_TABLE struct {
	CbSize                uint32
	RIOReceive            uintptr
	RIOReceiveEx          uintptr
	RIOSend               uintptr
	RIOSendEx             uintptr
	CloseCompletionQueue  uintptr
	CreateCompletionQueue uintptr
	CreateRequestQueue    uintptr
	DequeueCompletion     uintptr
	DeregisterBuffer      uintptr
	RegisterBuffer        uintptr
	ResizeCompletionQueue uintptr
	ResizeRequestQueue    uintptr
}

func init() {
	if err := initRIOFunctionTable(); err == nil && rioRegisterBufFn != 0 {
		rioAvailable.Store(true)
		return
	}

	if procVirtualLock.Find() == nil && procVirtualUnlock.Find() == nil {
		rioAvailable.Store(true)
	}
}

func initRIOFunctionTable() error {
	sock, err := windows.Socket(windows.AF_INET, windows.SOCK_STREAM, windows.IPPROTO_TCP)
	if err != nil {
		return err
	}

	defer func() { _ = windows.Closesocket(sock) }()

	guid := windows.GUID{
		Data1: 0x8509b001,
		Data2: 0x9604,
		Data3: 0x4060,
		Data4: [8]byte{0x96, 0xb7, 0xe6, 0x0b, 0x45, 0x22, 0x8a, 0x35},
	}

	var table RIO_EXTENSION_FUNCTION_TABLE

	table.CbSize = uint32(unsafe.Sizeof(table))

	var bytesReturned uint32

	err = windows.WSAIoctl(
		sock,
		0x40047424,
		(*byte)(unsafe.Pointer(&guid)),
		uint32(unsafe.Sizeof(guid)),
		(*byte)(unsafe.Pointer(&table)),
		uint32(unsafe.Sizeof(table)),
		&bytesReturned,
		nil,
		0,
	)
	if err != nil {
		return err
	}

	rioRegisterBufFn = table.RegisterBuffer
	rioDeregisterBufFn = table.DeregisterBuffer

	return nil
}

// BufferRegistration tracks a memory slice locked in physical RAM or registered with Windows RIO.
type BufferRegistration struct {
	BufferID uintptr
	Data     []byte
	IsRIO    bool
}

// IsRIOSupported reports whether Windows Registered I/O (RIO) extensions or VirtualLock are available.
func IsRIOSupported() bool {
	return rioAvailable.Load()
}

// RegisterBuffer registers data with Windows RIO (or locks in RAM via VirtualLock) to eliminate page faults during socket I/O.
func RegisterBuffer(data []byte) (*BufferRegistration, error) {
	if len(data) == 0 {
		return nil, nil
	}

	if !IsRIOSupported() {
		return nil, ErrRIONotSupported
	}

	if rioRegisterBufFn != 0 {
		r1, _, err := syscall.SyscallN(
			rioRegisterBufFn,
			uintptr(unsafe.Pointer(&data[0])),
			uintptr(uint32(len(data))),
		)

		if r1 != 0 && r1 != ^uintptr(0) {
			return &BufferRegistration{
				BufferID: r1,
				Data:     data,
				IsRIO:    true,
			}, nil
		}

		_ = err
	}

	r1, _, err := procVirtualLock.Call(
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
	)
	if r1 == 0 {
		if err != nil && !errors.Is(err, syscall.Errno(0)) {
			return nil, err
		}

		return nil, ErrRIOFailed
	}

	return &BufferRegistration{
		BufferID: uintptr(unsafe.Pointer(&data[0])),
		Data:     data,
		IsRIO:    false,
	}, nil
}

// Deregister unlocks or unregisters the memory buffer from the Windows kernel.
func (b *BufferRegistration) Deregister() {
	if b == nil || b.BufferID == 0 || b.BufferID == ^uintptr(0) {
		return
	}

	if b.IsRIO && rioDeregisterBufFn != 0 {
		_, _, _ = syscall.SyscallN(rioDeregisterBufFn, b.BufferID)
	} else if len(b.Data) > 0 {
		_, _, _ = procVirtualUnlock.Call(
			uintptr(unsafe.Pointer(&b.Data[0])),
			uintptr(len(b.Data)),
		)
	}

	b.BufferID = 0
	b.Data = nil
}
