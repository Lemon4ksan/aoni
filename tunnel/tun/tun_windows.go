// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package tun

import (
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modwintun                      = syscall.NewLazyDLL("wintun.dll")
	procWintunCreateAdapter        = modwintun.NewProc("WintunCreateAdapter")
	procWintunCloseAdapter         = modwintun.NewProc("WintunCloseAdapter")
	procWintunStartSession         = modwintun.NewProc("WintunStartSession")
	procWintunEndSession           = modwintun.NewProc("WintunEndSession")
	procWintunGetReadWaitEvent     = modwintun.NewProc("WintunGetReadWaitEvent")
	procWintunReceivePacket        = modwintun.NewProc("WintunReceivePacket")
	procWintunReleaseReceivePacket = modwintun.NewProc("WintunReleaseReceivePacket")
	procWintunAllocateSendPacket   = modwintun.NewProc("WintunAllocateSendPacket")
	procWintunSendPacket           = modwintun.NewProc("WintunSendPacket")
)

var (
	// ErrWintunNotLoaded indicates that wintun.dll is missing from the system path.
	ErrWintunNotLoaded = errors.New("aoni/tun: wintun.dll not found in application or system directory")

	// ErrAdapterCreationFailed indicates WintunCreateAdapter failed to register the virtual interface.
	ErrAdapterCreationFailed = errors.New("aoni/tun: failed to create wintun network adapter")

	// ErrSessionCreationFailed indicates WintunStartSession failed to allocate ring-buffers.
	ErrSessionCreationFailed = errors.New("aoni/tun: failed to start wintun session")
)

const defaultRingCapacity uint32 = 0x400000 // 4MB Ring Buffer Capacity

// WintunAdapter encapsulates a Windows Wintun L3 network interface session.
type WintunAdapter struct {
	adapterHandle uintptr
	sessionHandle uintptr
	waitEvent     windows.Handle
	closed        atomic.Bool
}

// NewWintunAdapter creates and initializes a Wintun Layer 3 network interface on Windows.
//
// Preconditions:
//   - Requires wintun.dll to be located in the executable directory.
//   - Requires administrator privileges on Windows to register L3 network adapters.
func NewWintunAdapter(adapterName, tunnelType string) (*WintunAdapter, error) {
	if err := modwintun.Load(); err != nil {
		return nil, ErrWintunNotLoaded
	}

	nameUtf16, err := windows.UTF16PtrFromString(adapterName)
	if err != nil {
		return nil, err
	}

	typeUtf16, err := windows.UTF16PtrFromString(tunnelType)
	if err != nil {
		return nil, err
	}

	r1, _, errCall := procWintunCreateAdapter.Call(
		uintptr(unsafe.Pointer(nameUtf16)),
		uintptr(unsafe.Pointer(typeUtf16)),
		0,
	)

	if r1 == 0 {
		return nil, fmt.Errorf("%w: %w", ErrAdapterCreationFailed, errCall)
	}

	adapter := &WintunAdapter{adapterHandle: r1}

	if err := adapter.startSession(defaultRingCapacity); err != nil {
		_ = adapter.Close()
		return nil, err
	}

	return adapter, nil
}

func (a *WintunAdapter) startSession(capacity uint32) error {
	r1, _, errCall := procWintunStartSession.Call(a.adapterHandle, uintptr(capacity))
	if r1 == 0 {
		return fmt.Errorf("%w: %w", ErrSessionCreationFailed, errCall)
	}

	a.sessionHandle = r1

	eventHandle, _, _ := procWintunGetReadWaitEvent.Call(a.sessionHandle)
	a.waitEvent = windows.Handle(eventHandle)

	return nil
}

// ReceivePacket reads one Layer 3 IP packet directly from Wintun's shared ring-buffer.
//
// Postconditions:
//   - Callers MUST call ReleaseReceivePacket after reading packet payload to free ring capacity.
func (a *WintunAdapter) ReceivePacket() ([]byte, error) {
	for {
		if a.closed.Load() {
			return nil, io.EOF
		}

		var size uint32

		r1, _, _ := procWintunReceivePacket.Call(a.sessionHandle, uintptr(unsafe.Pointer(&size)))
		if r1 != 0 {
			//nolint:govet // r1 points to C/driver memory allocated by wintun.dll outside Go GC heap
			return unsafe.Slice((*byte)(unsafe.Pointer(r1)), size), nil
		}

		// Wait on OS wait-event if ring buffer is currently empty
		_, _ = windows.WaitForSingleObject(a.waitEvent, 100)
	}
}

// ReleaseReceivePacket releases internal ring-buffer slice returned by ReceivePacket.
func (a *WintunAdapter) ReleaseReceivePacket(pkt []byte) {
	if len(pkt) == 0 || a.closed.Load() {
		return
	}

	ptr := uintptr(unsafe.Pointer(&pkt[0]))
	_, _, _ = procWintunReleaseReceivePacket.Call(a.sessionHandle, ptr)
}

// SendPacket allocates ring capacity and writes an IP packet back to the Windows network stack.
func (a *WintunAdapter) SendPacket(packet []byte) error {
	if a.closed.Load() || len(packet) == 0 {
		return nil
	}

	size := uintptr(len(packet))

	r1, _, errCall := procWintunAllocateSendPacket.Call(a.sessionHandle, size)
	if r1 == 0 {
		return fmt.Errorf("aoni tun: allocate send packet failed: %w", errCall)
	}

	//nolint:govet // r1 points to C/driver memory allocated by wintun.dll outside Go GC heap
	dst := unsafe.Slice((*byte)(unsafe.Pointer(r1)), len(packet))
	copy(dst, packet)

	_, _, _ = procWintunSendPacket.Call(a.sessionHandle, r1)

	return nil
}

// Close gracefully ends Wintun session and destroys network adapter.
func (a *WintunAdapter) Close() error {
	if a.closed.CompareAndSwap(false, true) {
		if a.sessionHandle != 0 {
			_, _, _ = procWintunEndSession.Call(a.sessionHandle)
		}

		if a.adapterHandle != 0 {
			_, _, _ = procWintunCloseAdapter.Call(a.adapterHandle)
		}
	}

	return nil
}
