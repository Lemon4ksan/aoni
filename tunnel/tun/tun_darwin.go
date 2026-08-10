// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin

package tun

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	afSystem        = 32         // AF_SYSTEM on macOS
	afSysControl    = 2          // AF_SYS_CONTROL sub-address type
	sysprotoControl = 2          // SYSPROTO_CONTROL protocol number
	ctliocginfo     = 0xc0644e03 // CTLIOCGINFO ioctl command (_IOWR('N', 3, struct ctl_info))
	utunOptIfname   = 2          // UTUN_OPT_IFNAME getsockopt option
	utunControlName = "com.apple.net.utun_control"
)

var (
	// ErrDarwinUtunFailed indicates that utun interface creation failed on macOS.
	ErrDarwinUtunFailed = errors.New("aoni/tun: failed to create macOS utun interface")

	// ErrInvalidUtunName indicates an interface name that does not match utun[0-9]+ format.
	ErrInvalidUtunName = errors.New("aoni/tun: interface name must match utun[0-9]+")
)

type ctlInfo struct {
	CtlID   uint32
	CtlName [96]byte
}

type sockaddrCtl struct {
	ScLen      uint8
	ScFamily   uint8
	ScSysAddr  uint16
	ScID       uint32
	ScUnit     uint32
	ScReserved [5]uint32
}

// DarwinAdapter encapsulates a macOS utun virtual network interface.
type DarwinAdapter struct {
	file *os.File
	name string
}

// NewDarwinAdapter creates and registers a Layer 3 utun interface on macOS without CGO.
// Requires root or sudo privileges on macOS to allocate system control sockets.
func NewDarwinAdapter(devName string) (*DarwinAdapter, error) {
	unit := 0
	if devName != "" {
		after, found := strings.CutPrefix(devName, "utun")
		if !found {
			return nil, ErrInvalidUtunName
		}

		parsedUnit, err := strconv.Atoi(after)
		if err != nil {
			return nil, ErrInvalidUtunName
		}

		unit = parsedUnit + 1 // 1-based unit index for macOS utun
	}

	fd, err := syscall.Socket(afSystem, syscall.SOCK_DGRAM, sysprotoControl)
	if err != nil {
		return nil, fmt.Errorf("%w: socket creation failed: %v", ErrDarwinUtunFailed, err)
	}

	var info ctlInfo
	copy(info.CtlName[:], utunControlName)

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(ctliocginfo),
		uintptr(unsafe.Pointer(&info)),
	)

	if errno != 0 {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("%w: ioctl CTLIOCGINFO failed: %v", ErrDarwinUtunFailed, errno)
	}

	sa := sockaddrCtl{
		ScLen:     uint8(unsafe.Sizeof(sockaddrCtl{})), // 32 bytes struct size
		ScFamily:  afSystem,
		ScSysAddr: afSysControl,
		ScID:      info.CtlID, // Correct Kernel Controller ID
		ScUnit:    uint32(unit),
	}

	_, _, errno = syscall.Syscall(
		syscall.SYS_CONNECT,
		uintptr(fd),
		uintptr(unsafe.Pointer(&sa)),
		uintptr(sa.ScLen),
	)

	if errno != 0 {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("%w: connect utun failed: %v", ErrDarwinUtunFailed, errno)
	}

	var ifNameBuf [20]byte

	ifNameLen := uint32(len(ifNameBuf))

	_, _, errno = syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(sysprotoControl),
		uintptr(utunOptIfname),
		uintptr(unsafe.Pointer(&ifNameBuf)),
		uintptr(unsafe.Pointer(&ifNameLen)),
		0,
	)

	if errno != 0 {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("%w: getsockopt UTUN_OPT_IFNAME failed: %v", ErrDarwinUtunFailed, errno)
	}

	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("%w: set nonblock failed: %v", ErrDarwinUtunFailed, err)
	}

	realName := cStringToGoString(ifNameBuf[:ifNameLen])
	file := os.NewFile(uintptr(fd), realName)

	return &DarwinAdapter{
		file: file,
		name: realName,
	}, nil
}

func cStringToGoString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}

	return string(b)
}

// Name returns the actual utun interface name assigned by macOS kernel (e.g. "utun0").
func (a *DarwinAdapter) Name() string {
	return a.name
}

// Read reads one Layer 3 IP packet from the macOS kernel utun interface.
func (a *DarwinAdapter) Read(b []byte) (int, error) {
	return a.file.Read(b)
}

// Write transmits an IP packet back into the macOS kernel network stack.
func (a *DarwinAdapter) Write(b []byte) (int, error) {
	return a.file.Write(b)
}

// Close releases the file descriptor and destroys the macOS utun interface.
func (a *DarwinAdapter) Close() error {
	return a.file.Close()
}
