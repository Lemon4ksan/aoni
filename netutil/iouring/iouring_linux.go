// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

// Package iouring implements a native, zero-allocation Linux io_uring kernel ring-buffer engine.
package iouring

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

var (
	// ErrIOUringUnsupported is returned on non-Linux platforms where io_uring is unavailable.
	ErrIOUringUnsupported = errors.New("aoni/iouring: io_uring is only supported on Linux")

	// ErrRingFull is returned when no submission queue entries are available.
	ErrRingFull = errors.New("aoni/iouring: submission queue is full")

	// ErrRingClosed is returned when operations are attempted on a closed ring.
	ErrRingClosed = errors.New("aoni/iouring: ring is closed")
)

type sqRing struct {
	khead        *uint32
	ktail        *uint32
	kringMask    *uint32
	kringEntries *uint32
	kflags       *uint32
	kdropped     *uint32
	array        []uint32
	sqes         []SQE
	sqeHead      uint32
	sqeTail      uint32
	ringPtr      uintptr
	ringSize     uintptr
	sqesPtr      uintptr
	sqesSize     uintptr
}

type cqRing struct {
	khead        *uint32
	ktail        *uint32
	kringMask    *uint32
	kringEntries *uint32
	koverflow    *uint32
	cqes         []CQE
	ringPtr      uintptr
	ringSize     uintptr
}

// Ring encapsulates a Linux io_uring instance.
type Ring struct {
	mu      sync.Mutex
	fd      int
	flags   uint32
	sq      sqRing
	cq      cqRing
	closed  atomic.Bool
	entries uint32
}

// New creates a new [Ring] with the specified number of queue entries and optional setup flags.
func New(entries uint32, flags ...uint32) (*Ring, error) {
	if entries == 0 {
		entries = 64
	}

	var setupFlags uint32
	if len(flags) > 0 {
		setupFlags = flags[0]
	}

	var p ioUringParams
	p.flags = setupFlags

	r1, _, err := unix.Syscall(sysIOUringSetup, uintptr(entries), uintptr(unsafe.Pointer(&p)), 0)
	if err != 0 {
		return nil, fmt.Errorf("aoni/iouring: sys_io_uring_setup failed: %w", err)
	}

	ringFd := int(r1)
	r := &Ring{
		fd:      ringFd,
		flags:   p.flags,
		entries: p.sqEntries,
	}

	if err := r.mmapRings(&p); err != nil {
		_ = unix.Close(ringFd)
		return nil, err
	}

	return r, nil
}

func (r *Ring) mmapRings(p *ioUringParams) error {
	sqSize := uintptr(p.sqOff.array + p.sqEntries*4)
	cqSize := uintptr(p.cqOff.cqes + p.cqEntries*uint32(unsafe.Sizeof(CQE{})))

	// Some kernels share SQ and CQ memory
	if p.features&1 != 0 { // IORING_FEAT_SINGLE_MMAP
		if cqSize > sqSize {
			sqSize = cqSize
		}
		cqSize = sqSize
	}

	sqPtr, err := unix.Mmap(r.fd, IORING_OFF_SQ_RING, int(sqSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return fmt.Errorf("mmap SQ ring: %w", err)
	}

	r.sq.ringPtr = uintptr(unsafe.Pointer(&sqPtr[0]))
	r.sq.ringSize = sqSize

	r.sq.khead = (*uint32)(unsafe.Pointer(r.sq.ringPtr + uintptr(p.sqOff.head)))
	r.sq.ktail = (*uint32)(unsafe.Pointer(r.sq.ringPtr + uintptr(p.sqOff.tail)))
	r.sq.kringMask = (*uint32)(unsafe.Pointer(r.sq.ringPtr + uintptr(p.sqOff.ringMask)))
	r.sq.kringEntries = (*uint32)(unsafe.Pointer(r.sq.ringPtr + uintptr(p.sqOff.ringEntries)))
	r.sq.kflags = (*uint32)(unsafe.Pointer(r.sq.ringPtr + uintptr(p.sqOff.flags)))
	r.sq.kdropped = (*uint32)(unsafe.Pointer(r.sq.ringPtr + uintptr(p.sqOff.dropped)))

	arrayPtr := unsafe.Pointer(r.sq.ringPtr + uintptr(p.sqOff.array))
	r.sq.array = unsafe.Slice((*uint32)(arrayPtr), p.sqEntries)

	// Map SQEs
	sqesSize := uintptr(p.sqEntries) * unsafe.Sizeof(SQE{})
	sqesPtr, err := unix.Mmap(r.fd, IORING_OFF_SQES, int(sqesSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		_ = unix.Munmap(sqPtr)
		return fmt.Errorf("mmap SQEs: %w", err)
	}

	r.sq.sqesPtr = uintptr(unsafe.Pointer(&sqesPtr[0]))
	r.sq.sqesSize = sqesSize
	r.sq.sqes = unsafe.Slice((*SQE)(unsafe.Pointer(r.sq.sqesPtr)), p.sqEntries)

	// Map CQ ring
	var cqPtr []byte
	if p.features&1 != 0 {
		r.cq.ringPtr = r.sq.ringPtr
		r.cq.ringSize = r.sq.ringSize
	} else {
		cqPtr, err = unix.Mmap(r.fd, IORING_OFF_CQ_RING, int(cqSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
		if err != nil {
			_ = unix.Munmap(sqesPtr)
			_ = unix.Munmap(sqPtr)
			return fmt.Errorf("mmap CQ ring: %w", err)
		}
		r.cq.ringPtr = uintptr(unsafe.Pointer(&cqPtr[0]))
		r.cq.ringSize = cqSize
	}

	r.cq.khead = (*uint32)(unsafe.Pointer(r.cq.ringPtr + uintptr(p.cqOff.head)))
	r.cq.ktail = (*uint32)(unsafe.Pointer(r.cq.ringPtr + uintptr(p.cqOff.tail)))
	r.cq.kringMask = (*uint32)(unsafe.Pointer(r.cq.ringPtr + uintptr(p.cqOff.ringMask)))
	r.cq.kringEntries = (*uint32)(unsafe.Pointer(r.cq.ringPtr + uintptr(p.cqOff.ringEntries)))
	r.cq.koverflow = (*uint32)(unsafe.Pointer(r.cq.ringPtr + uintptr(p.cqOff.overflow)))

	cqesPtr := unsafe.Pointer(r.cq.ringPtr + uintptr(p.cqOff.cqes))
	r.cq.cqes = unsafe.Slice((*CQE)(cqesPtr), p.cqEntries)

	return nil
}

// GetSQE retrieves the next available submission queue entry for population.
// Must be called with lock held or by single owner thread.
func (r *Ring) GetSQE() (*SQE, error) {
	if r.closed.Load() {
		return nil, ErrRingClosed
	}

	head := atomic.LoadUint32(r.sq.khead)
	tail := r.sq.sqeTail

	if (tail - head) >= r.entries {
		return nil, ErrRingFull
	}

	index := tail & *r.sq.kringMask
	sqe := &r.sq.sqes[index]
	*sqe = SQE{} // Zero memory

	r.sq.array[index] = index
	r.sq.sqeTail = tail + 1

	return sqe, nil
}

// Submit flushes all populated SQEs to the kernel.
func (r *Ring) Submit() (int, error) {
	return r.SubmitAndWait(0)
}

// SubmitAndWait flushes SQEs and waits for at least waitNr completions.
func (r *Ring) SubmitAndWait(waitNr uint32) (int, error) {
	if r.closed.Load() {
		return 0, ErrRingClosed
	}

	toSubmit := r.sq.sqeTail - atomic.LoadUint32(r.sq.ktail)
	if toSubmit > 0 {
		atomic.StoreUint32(r.sq.ktail, r.sq.sqeTail)
	}

	var flags uint32
	if waitNr > 0 {
		flags |= IORING_ENTER_GETEVENTS
	}

	res, _, err := unix.Syscall6(
		sysIOUringEnter,
		uintptr(r.fd),
		uintptr(toSubmit),
		uintptr(waitNr),
		uintptr(flags),
		0,
		0,
	)
	if err != 0 {
		return 0, err
	}

	return int(res), nil
}

// WaitCQE blocks until a completion queue event is ready and returns it.
func (r *Ring) WaitCQE() (CQE, error) {
	for {
		if r.closed.Load() {
			return CQE{}, ErrRingClosed
		}

		head := atomic.LoadUint32(r.cq.khead)
		tail := atomic.LoadUint32(r.cq.ktail)

		if head != tail {
			index := head & *r.cq.kringMask
			cqe := r.cq.cqes[index]
			atomic.StoreUint32(r.cq.khead, head+1)
			return cqe, nil
		}

		if _, err := r.SubmitAndWait(1); err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return CQE{}, err
		}
	}
}

// Close unmaps kernel shared memory and closes the io_uring file descriptor.
func (r *Ring) Close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sq.sqesPtr != 0 {
		_ = unix.Munmap(unsafe.Slice((*byte)(unsafe.Pointer(r.sq.sqesPtr)), r.sq.sqesSize))
	}
	if r.sq.ringPtr != 0 {
		_ = unix.Munmap(unsafe.Slice((*byte)(unsafe.Pointer(r.sq.ringPtr)), r.sq.ringSize))
	}
	if r.cq.ringPtr != 0 && r.cq.ringPtr != r.sq.ringPtr {
		_ = unix.Munmap(unsafe.Slice((*byte)(unsafe.Pointer(r.cq.ringPtr)), r.cq.ringSize))
	}

	return unix.Close(r.fd)
}
