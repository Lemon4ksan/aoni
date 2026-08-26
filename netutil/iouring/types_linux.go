// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package iouring

const (
	sysIOUringSetup    = 425
	sysIOUringEnter    = 426
	sysIOUringRegister = 427
)

// Standard io_uring opcodes.
const (
	OpNop       = 0
	OpReadv     = 1
	OpWritev    = 2
	OpFsync     = 3
	OpRead      = 22
	OpWrite     = 23
	OpSend      = 26
	OpRecv      = 27
	OpConnect   = 16
	OpAccept    = 13
	OpClose     = 19
	OpSendZC    = 47
	OpSendMsgZC = 48
	OpTimeout   = 11
	OpShutdown  = 34
)

// Enter flags.
const (
	IORING_ENTER_GETEVENTS = 1 << 0
	IORING_ENTER_SQ_WAKEUP = 1 << 1
	IORING_ENTER_SQ_WAIT   = 1 << 2
	IORING_ENTER_EXT_ARG   = 1 << 3
)

// Setup flags.
const (
	IORING_SETUP_IOPOLL     = 1 << 0
	IORING_SETUP_SQPOLL     = 1 << 1
	IORING_SETUP_SQ_AFF     = 1 << 2
	IORING_SETUP_CQSIZE     = 1 << 3
	IORING_SETUP_CLAMP      = 1 << 4
	IORING_SETUP_ATTACH_WQ  = 1 << 5
	IORING_SETUP_R_DISABLED = 1 << 6
)

// Magic mmap offsets.
const (
	IORING_OFF_SQ_RING = 0
	IORING_OFF_CQ_RING = 0x8000000
	IORING_OFF_SQES    = 0x10000000
)

type ioSQOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	flags       uint32
	dropped     uint32
	array       uint32
	resv1       uint32
	userAddr    uint64
}

type ioCQOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	overflow    uint32
	cqes        uint32
	flags       uint32
	resv1       uint32
	userAddr    uint64
}

type ioUringParams struct {
	sqEntries    uint32
	cqEntries    uint32
	flags        uint32
	sqThreadCPU  uint32
	sqThreadIdle uint32
	features     uint32
	wqFD         uint32
	resv         [3]uint32
	sqOff        ioSQOffsets
	cqOff        ioCQOffsets
}

// SQE is an io_uring submission queue entry matching Linux struct io_uring_sqe (64 bytes).
type SQE struct {
	Opcode      uint8
	Flags       uint8
	IOPrio      uint16
	Fd          int32
	Off         uint64
	Addr        uint64
	Len         uint32
	OpFlags     uint32
	UserData    uint64
	BufIndex    uint16
	Personality uint16
	SpliceFdIn  int32
	Pad2        [2]uint64
}

// CQE is an io_uring completion queue entry matching Linux struct io_uring_cqe (16 bytes).
type CQE struct {
	UserData uint64
	Res      int32
	Flags    uint32
}
